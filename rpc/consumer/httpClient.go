package consumer

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"rpc_service/codec"
	"rpc_service/config"
	"rpc_service/global"
	"rpc_service/protocol"
	"strings"
	"sync"
	"time"
)

/*
HTTP 协议

demo:

	client : CONNECT 10.0.0.1:8080/_rpc_ HTTP/1.0
	server : HTTP/1.0 200 Connected to  RPC
*/
const (
	connected        = "200 Connected to  RPC"
	defaultRPCPath   = "/_prc_"
	defaultDebugPath = "/debug/rpc"
)

// Call is  message format for http
type Call struct {
	Seq           uint64 // request message sequence
	ServiceMethod string // format "<service>.<method>"
	Class         string
	Args          interface{} // arguments to the function
	Reply         interface{} // reply from the function
	Error         error       // if error occurs, it will be set
	Done          chan *Call  // Strobes when call is complete.
}

type HttpClient struct {
	codec        codec.Codec
	ClientOption *ClientOption
	header       *protocol.RPCMsg
	sending      sync.Mutex // protect following
	mu           sync.Mutex // protect following
	seq          uint64
	pending      map[uint64]*Call
	closing      bool // user has called Close
	shutdown     bool // server has told us to stop
	addr         string
}

var _ io.Closer = (*HttpClient)(nil)

var ErrShutdown = errors.New("connection is shut down")

func (call *Call) done() {
	call.Done <- call
}

// Close the connection
func (client *HttpClient) Close() error {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing {
		return ErrShutdown
	}
	client.closing = true
	return client.codec.(*codec.GobCodec).Close()
}

// IsAvailable return true if the client does work
func (client *HttpClient) IsAvailable() bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	return !client.shutdown && !client.closing
}

func (client *HttpClient) registerCall(call *Call) (uint64, error) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.closing || client.shutdown {
		return 0, ErrShutdown
	}
	call.Seq = client.seq
	client.pending[call.Seq] = call
	client.seq++
	return call.Seq, nil
}

func (client *HttpClient) removeCall(seq uint64) *Call {
	client.mu.Lock()
	defer client.mu.Unlock()
	call := client.pending[seq]
	delete(client.pending, seq)
	return call
}

func (client *HttpClient) terminateCalls(err error) {
	client.sending.Lock()
	defer client.sending.Unlock()
	client.mu.Lock()
	defer client.mu.Unlock()
	client.shutdown = true
	for _, call := range client.pending {
		call.Error = err
		call.done()
	}
}

func (client *HttpClient) send(call *Call) {
	// make sure that the client will send a complete request
	client.sending.Lock()
	defer client.sending.Unlock()

	// register this call.
	seq, err := client.registerCall(call)
	if err != nil {
		call.Error = err
		call.done()
		return
	}

	// prepare request header
	msg := protocol.NewRPCMsg()
	msg.SetVersion(config.Protocol_MsgVersion)
	msg.SetMsgType(protocol.Request)
	msg.SetCompressType(client.ClientOption.CompressType)
	msg.SetSerializeType(client.ClientOption.SerializeType)
	msg.ServiceMethod = call.ServiceMethod
	msg.ServiceClass = call.Class

	// encode and send the request
	if err := client.codec.(*codec.GobCodec).Write(msg, call.Args); err != nil {
		call := client.removeCall(seq)
		// call may be nil, it usually means that Write partially failed,
		// client has received the response and handled
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

func (client *HttpClient) receive() {
	var err error
	for err == nil {
		var h *protocol.RPCMsg
		// gob
		if err = client.codec.(*codec.GobCodec).ReadHeader(h); err != nil {
			break
		}
		call := client.removeCall(h.GetMessageID())
		switch {
		case call == nil:
			// it usually means that Write partially failed
			// and call was already removed.
			err = client.codec.(*codec.GobCodec).ReadBody(nil)
		case h.Error != nil:
			call.Error = h.Error
			err = client.codec.(*codec.GobCodec).ReadBody(nil)
			call.done()
		default:
			err = client.codec.(*codec.GobCodec).ReadBody(call.Reply)
			if err != nil {
				call.Error = errors.New("reading body " + err.Error())
			}
			call.done()
		}
	}
	// error occurs, so terminateCalls pending calls
	client.terminateCalls(err)
}

// Go invokes the function asynchronously.
// It returns the Call structure representing the invocation.
func (client *HttpClient) Go(serviceMethod string, args, reply interface{}, done chan *Call) *Call {
	if done == nil {
		done = make(chan *Call, 10)
	} else if cap(done) == 0 {
		log.Panic("rpc client: done channel is unbuffered")
	}
	call := &Call{
		ServiceMethod: serviceMethod,
		Args:          args,
		Reply:         reply,
		Done:          done,
	}
	client.send(call)
	return call
}

// Call invokes the named function, waits for it to complete,
// and returns its error status.
// ----- use for httpclient
func (client *HttpClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	call := client.Go(serviceMethod, args, reply, make(chan *Call, 1))
	select {
	case <-ctx.Done():
		client.removeCall(call.Seq)
		return errors.New("rpc client: call failed: " + ctx.Err().Error())
	case call := <-call.Done:
		return call.Error
	}
}

func NewAClient(conn net.Conn, opt *ClientOption) (*HttpClient, error) {
	f := global.NewCodecFuncMap[opt.SerializeType]
	if f == nil {
		err := fmt.Errorf("invalid codec type %s", opt.SerializeType)
		log.Println("rpc client: codec error:", err)
		return nil, err
	}
	// send ClientOption with server
	if err := json.NewEncoder(conn).Encode(opt); err != nil {
		log.Println("rpc client: ClientOption error: ", err)
		_ = conn.Close()
		return nil, err
	}
	return newClientCodec(f(conn), opt), nil
}

func newClientCodec(codec codec.Codec, opt *ClientOption) *HttpClient {
	client := &HttpClient{
		seq:          1, // seq starts with 1, 0 means invalid call
		codec:        codec,
		ClientOption: opt,
		pending:      make(map[uint64]*Call),
	}
	go client.receive()
	return client
}

type clientResult struct {
	client *HttpClient
	err    error
}

func parseClientOption(opts ...*ClientOption) (*ClientOption, error) {
	// if opts is nil or pass nil as parameter
	if len(opts) == 0 || opts[0] == nil {
		return &DefaultClientOption, nil
	}
	if len(opts) != 1 {
		return nil, errors.New("number of ClientOption is more than 1")
	}
	opt := opts[0]
	opt.MagicNumber = DefaultClientOption.MagicNumber

	return opt, nil
}

type newClientFunc func(conn net.Conn, opt *ClientOption) (client *HttpClient, err error)

func dialTimeout(f newClientFunc, network, address string, opts ...*ClientOption) (client *HttpClient, err error) {
	opt, err := parseClientOption(opts...)
	if err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout(network, address, opt.ConnectionTimeout)
	if err != nil {
		return nil, err
	}
	// close the connection if client is nil
	defer func() {
		if err != nil {
			_ = conn.Close()
		}
	}()
	ch := make(chan clientResult)
	go func() {
		client, err := f(conn, opt)
		ch <- clientResult{client: client, err: err}
	}()
	if opt.ConnectionTimeout == 0 {
		result := <-ch
		return result.client, result.err
	}
	select {
	case <-time.After(opt.ConnectionTimeout):
		return nil, fmt.Errorf("rpc client: connect timeout: expect within %s", opt.ConnectionTimeout)
	case result := <-ch:
		return result.client, result.err
	}
}

// Dial connects to an RPC server at the specified network address
func Dial(network, address string, opts ...*ClientOption) (*HttpClient, error) {
	return dialTimeout(NewAClient, network, address, opts...)
}

// NewHTTPClient new a Client instance via HTTP as transport protocol
func NewHTTPClient(conn net.Conn, opt *ClientOption) (*HttpClient, error) {
	_, _ = io.WriteString(conn, fmt.Sprintf("CONNECT %s HTTP/1.0\n\n", defaultRPCPath))

	// Require successful HTTP response
	// before switching to RPC msg.
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: "CONNECT"})
	if err == nil && resp.Status == connected {
		return NewAClient(conn, opt)
	}
	if err == nil {
		err = errors.New("unexpected HTTP response: " + resp.Status)
	}
	return nil, err
}

// DialHTTP connects to an HTTP RPC server at the specified network address
// listening on the default HTTP RPC path.
func DialHTTP(network, address string, opts ...*ClientOption) (*HttpClient, error) {
	return dialTimeout(NewHTTPClient, network, address, opts...)
}

// XDial calls different functions to connect to a RPC server
// according the first parameter rpcAddr.
// rpcAddr is a general format (protocol@addr) to represent a rpc server
// eg, http@10.0.0.1:7001, tcp@10.0.0.1:9999, unix@/tmp/rpc.sock
func XDial(rpcAddr string, opts ...*ClientOption) (*HttpClient, error) {
	parts := strings.Split(rpcAddr, "@")
	if len(parts) != 2 {
		return nil, fmt.Errorf("rpc client err: wrong format '%s', expect protocol@addr", rpcAddr)
	}
	protocol, addr := parts[0], parts[1]
	switch protocol {
	case "http":
		return DialHTTP("tcp", addr, opts...)
	default:
		// tcp, unix or other transport protocol
		return Dial(protocol, addr, opts...)
	}
}
