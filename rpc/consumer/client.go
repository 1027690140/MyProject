package consumer

import (
	"context"
	"encoding/base64"
	"fmt"

	//"errors"
	//"fmt"
	"log"
	"net"
	"reflect"
	"rpc_service/config"
	"rpc_service/global"
	"rpc_service/protocol"
	"rpc_service/provider"
	"rpc_service/trace"
	"time"
)

type Client interface {
	Connect(string) error
	//直接执行
	Invoke(context.Context, *Service, interface{}, ...interface{}) (interface{}, *global.StatusError)
	//构造执行方法
	MakeFunc(context.Context, *Service, interface{})
	Close()
	GetAddr() string
}

type ClientOption struct {
	MagicNumber       byte
	Retries           int
	FailMode          FailMode
	ConnectionTimeout time.Duration
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	SerializeType     protocol.SerializeType
	CompressType      protocol.CompressType
	NetProtocol       string //"tcp", "udp",   http or quic(todo)
	LoadBalanceMode   LoadBalanceMode
	ConnPool          bool //是否使用连接池
	AuthConfig        *provider.AuthConfig
}

var DefaultClientOption = ClientOption{
	MagicNumber:       0x06,
	Retries:           3,
	FailMode:          Failover,
	ConnectionTimeout: 5 * time.Second,
	ReadTimeout:       1 * time.Second,
	WriteTimeout:      1 * time.Second,
	SerializeType:     protocol.Gob,
	CompressType:      protocol.None,
	NetProtocol:       "tcp",
	LoadBalanceMode:   RoundRobinBalance,
	ConnPool:          false,
	AuthConfig:        &provider.AuthConfig{RequireTransportSecurity: false},
}

var _ Client = new(RPCClient)

// RPCClient client
type RPCClient struct {
	conn         net.Conn
	ClientOption ClientOption
	addr         string
}

func NewClient(ClientOption ClientOption) Client {
	return &RPCClient{ClientOption: ClientOption}
}

// Connect "tcp", "tcp4", "tcp6":
func (cli *RPCClient) Connect(addr string) error {
	conn, err := net.DialTimeout(cli.ClientOption.NetProtocol, addr, cli.ClientOption.ConnectionTimeout)
	if err != nil {
		return err
	}
	cli.conn = conn
	cli.addr = addr
	return nil
}

// Invoke 发起调用
func (cli *RPCClient) Invoke(ctx context.Context, service *Service, stub interface{}, params ...interface{}) (interface{}, *global.StatusError) {
	//make func : this step can be prepared before invoke and store into cache
	cli.MakeFunc(ctx, service, stub)
	//reflect call
	return cli.wrapCall(ctx, stub, params...)
}

// Close 关闭连接
func (cli *RPCClient) Close() {
	if cli.conn != nil {
		cli.conn.Close()
	}
}

// GetAddr 获取当前连接的地址
func (cli *RPCClient) GetAddr() string {
	//cli.conn.RemoteAddr().String()
	return cli.addr
}

// MakeFunc 通过反射生成代理函数，
// 网络连接、请求数据序列化、网络传输、响应返回数据解析等工作都在代理函数中完成
func (cli *RPCClient) MakeFunc(ctx context.Context, service *Service, methodPtr interface{}) {
	traceinfo := ctx.Value("trace_info").(*trace.TraceInfo)

	container := reflect.ValueOf(methodPtr).Elem() //反射获取函数元素

	coder := global.Codecs[cli.ClientOption.SerializeType]

	//代理函数
	handler := func(req []reflect.Value) []reflect.Value {
		//出参个数
		numOut := container.Type().NumOut()

		//error
		errorHandler := func(err error) []reflect.Value {
			outArgs := make([]reflect.Value, numOut)
			for i := 0; i < len(outArgs)-1; i++ {
				outArgs[i] = reflect.Zero(container.Type().Out(i))
			}
			outArgs[len(outArgs)-1] = reflect.ValueOf(&err).Elem()
			return outArgs
		}

		//入参
		inArgs := make([]interface{}, 0, len(req))
		for _, arg := range req {
			inArgs = append(inArgs, arg.Interface())
		}

		payload, err := coder.Encode(inArgs) //[]byte
		if err != nil {
			log.Printf("encode err:%v\n", err)
			eif := &trace.ErrorInfo{
				Message: fmt.Sprint("encode err:%v\n", err),
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			Event := &trace.Event{
				Timestamp: time.Now().UnixNano(),
				Name:      "encode err",
				Data:      []interface{}{inArgs},
			}
			traceinfo.Events = append(traceinfo.Events, Event)
			traceinfo.EditTimestamp = time.Now().UnixNano()
			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			return errorHandler(err)
		}

		//send request
		startTime := time.Now()
		if cli.ClientOption.WriteTimeout != 0 {
			cli.conn.SetWriteDeadline(startTime.Add(cli.ClientOption.WriteTimeout))
		}
		msg := protocol.NewRPCMsg()
		msg.SetVersion(config.Protocol_MsgVersion)
		msg.SetMsgType(protocol.Request)
		msg.SetCompressType(cli.ClientOption.CompressType)
		msg.SetSerializeType(cli.ClientOption.SerializeType)
		msg.ServiceClass = service.Class
		msg.ServiceMethod = service.Method
		msg.ServiceAppID = service.AppId
		msg.Payload = payload
		//auth
		switch {
		case cli.ClientOption.AuthConfig.AuthType == provider.Basic:
			authHeader := "Basic " + base64.StdEncoding.EncodeToString([]byte(cli.ClientOption.AuthConfig.Username+":"+cli.ClientOption.AuthConfig.Password))
			msg.Metadata["Authorization"] = authHeader
		case cli.ClientOption.AuthConfig.AuthType == provider.JWT:
			msg.Metadata["Authorization"] = "JWT:" + string(cli.ClientOption.AuthConfig.Token)

		}
		//Send request
		err = msg.Send(cli.conn)
		if err != nil {
			log.Printf("send err:%v\n", err)
			eif := &trace.ErrorInfo{
				Message: fmt.Sprint("send err:%v\n", err),
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			Event := &trace.Event{
				Timestamp: time.Now().UnixNano(),
				Name:      "send err",
				Data:      []interface{}{cli.conn},
			}
			traceinfo.Events = append(traceinfo.Events, Event)
			traceinfo.EditTimestamp = time.Now().UnixNano()
			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			return errorHandler(err)
		}
		log.Println("send success!")

		// read response
		if cli.ClientOption.ReadTimeout != 0 {
			cli.conn.SetReadDeadline(startTime.Add(cli.ClientOption.ReadTimeout))
		}
		respMsg, err := protocol.Read(cli.conn)
		if err != nil {
			return errorHandler(err)
		}
		log.Println("response success!")

		//decode response
		respDecode := make([]interface{}, 0)
		err = coder.Decode(respMsg.Payload, &respDecode)
		if err != nil {
			log.Printf("decode err:%v\n", err)
			eif := &trace.ErrorInfo{
				Message: fmt.Sprint("decode err:%v\n", err),
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			Event := &trace.Event{
				Timestamp: time.Now().UnixNano(),
				Name:      "decode err",
				Data:      []interface{}{respMsg.Payload, &respDecode},
			}
			traceinfo.Events = append(traceinfo.Events, Event)
			traceinfo.EditTimestamp = time.Now().UnixNano()
			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			return errorHandler(err)
		}
		log.Println("decode success!")

		//output result
		if len(respDecode) == 0 {
			respDecode = make([]interface{}, numOut)
		}
		outArgs := make([]reflect.Value, numOut)
		for i := 0; i < numOut; i++ {
			if i != numOut { //处理非error
				if respDecode[i] == nil {
					outArgs[i] = reflect.Zero(container.Type().Out(i))
				} else {
					outArgs[i] = reflect.ValueOf(respDecode[i])
				}
			} else { //处理error
				outArgs[i] = reflect.Zero(container.Type().Out(i))
			}
		}
		traceinfo.EditTimestamp = time.Now().UnixNano()
		go trace.ArchiveAndPersistTraceInfo(traceinfo)
		return outArgs
	}

	//构造函数
	container.Set(reflect.MakeFunc(container.Type(), handler))
}

func (cli *RPCClient) wrapCall(ctx context.Context, stub interface{}, params ...interface{}) (interface{}, *global.StatusError) {
	f := reflect.ValueOf(stub).Elem()
	if len(params) != f.Type().NumIn() {
		//参数个数不匹配 返回错误
		return nil, &global.StatusError{Code: global.NewError(22), Message: "the number of params is not adapted"}
	}

	in := make([]reflect.Value, len(params))
	for idx, param := range params {
		in[idx] = reflect.ValueOf(param)
	}
	// f.Call是通过反射调用函数，并传入参数in
	result := f.Call(in)
	return result, &global.StatusError{}
}
