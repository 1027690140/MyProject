package provider

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sync/atomic"
	"time"

	"rpc_service/config"
	"rpc_service/global"
	"rpc_service/protocol"
)

var ServerClosedErr = errors.New("server closed error!")

var _ Listener = new(RPCListener)

// Listener is the interface that wraps the basic Run method.
type Listener interface {
	Run() error
	SetHandler(string, Handler)
	SetPlugins(PluginContainer)
	Close()
	GetAddrs() []string
	Shutdown()
}

// RPCListener base on tcp
type RPCListener struct {
	ServiceIP    string
	ServicePort  int
	ServerOption ServerOption
	Plugins      PluginContainer
	Handlers     map[string]Handler
	nl           net.Listener
	doneChan     chan struct{} //外层控制结束通道
	handlingNum  int32         //处理中任务数
	shutdown     int32         //关闭处理中标志位
}

// NewRPCListener 创建一个新的RPCListener
func NewRPCListener(ServerOption ServerOption) *RPCListener {
	return &RPCListener{ServiceIP: ServerOption.Ip,
		ServicePort:  ServerOption.Port,
		ServerOption: ServerOption,
		Handlers:     make(map[string]Handler),
		doneChan:     make(chan struct{}),
	}
}

// SetPlugins 设置插件
func (l *RPCListener) SetPlugins(plugins PluginContainer) {
	l.Plugins = plugins
}

// SetHandler 设置Handler
func (l *RPCListener) SetHandler(name string, handler Handler) {
	if _, ok := l.Handlers[name]; ok {
		log.Printf("%s is registered!\n", name)
		return
	}
	l.Handlers[name] = handler
}

// Run 监听等待连接
func (l *RPCListener) Run() error {
	//listen on port by tcp
	addr := fmt.Sprintf("%s:%d", l.ServiceIP, l.ServicePort)
	log.Println(l.ServerOption.NetProtocol, addr)
	netListener, err := net.Listen(l.ServerOption.NetProtocol, addr)
	if err != nil {
		//panic(err)
		return err
	}
	l.nl = netListener
	log.Printf("listen on %s success!", addr)

	//accept conn
	go l.acceptConn()
	return nil
}

func (l *RPCListener) acceptConn() {
	for {
		conn, err := l.nl.Accept()
		if err != nil {
			select { //done
			case <-l.getDoneChan():
				log.Println("server closed done")
				return
			default:
			}

			if e, ok := err.(net.Error); ok && e.Temporary() { //网络发生临时错误,不退出重试
				log.Printf("server accept network error: %v", err)
				time.Sleep(5 * time.Millisecond)
				continue
			}

			log.Printf("server accept err: %v\n", err)
			return
		}

		//plugin aop
		conn, ok := l.Plugins.ConnAcceptHook(conn)
		if !ok {
			//ConnAcceptHook 插件处理 conn 返回 false，说明连接不被接受。因此，执行 conn.Close() 关闭该连接，继续执行下一次循环，处理下一个连接
			if conn != nil {
				conn.Close()
			}
			continue
		}
		log.Printf("server accepted conn: %v\n", conn.RemoteAddr().String())

		//create new routine worker each connection
		go l.handleConn(conn)
	}
}

// handle each connection
func (l *RPCListener) handleConn(conn net.Conn) {
	//关闭
	if l.isShutdown() {
		return
	}

	//catch panic
	defer func() {
		if err := recover(); err != nil {
			log.Printf("server %s catch panic err:%s\n", conn.RemoteAddr(), err)
		}
		l.CloseConn(conn)
	}()

	for {
		//关闭
		if l.isShutdown() {
			return
		}

		//readtimeout
		startTime := time.Now()
		if l.ServerOption.ReadTimeout != 0 {
			conn.SetReadDeadline(startTime.Add(l.ServerOption.ReadTimeout))
		}

		//处理中任务数+1
		atomic.AddInt32(&l.handlingNum, 1)
		//任意退出都会导致处理中任务数-1
		defer atomic.AddInt32(&l.handlingNum, -1)

		//read from network
		msg, err := l.receiveData(conn)
		if err != nil || msg == nil {
			log.Println("server receive error:", err) //timeout
			return
		}

		//decode
		// l.Plugins.BeforeDecodeHook(msg)
		coder := global.Codecs[msg.Header.SerializeType()] // 选择解码器
		if coder == nil {
			return
		}
		// l.Plugins.BeforeDecodeArgHook(msg)
		// 调用方法的入参
		inArgs := make([]interface{}, 0)
		err = coder.Decode(msg.Payload, &inArgs) //rpcdata
		if err != nil {
			log.Println("server request decode err:%v\n", err)
			return
		}

		//call local service
		handler, ok := l.Handlers[msg.ServiceClass]
		if !ok {
			log.Println("server can not found handler error:", msg.ServiceClass)
			return
		}

		l.Plugins.BeforeCallHook(msg.ServiceClass, msg.ServiceMethod, inArgs)

		result, err := handler.Handle(msg.ServiceMethod, inArgs)

		l.Plugins.AfterCallHook(msg.ServiceClass, msg.ServiceMethod, inArgs, result, err)

		//encode
		encodeRes, err := coder.Encode(result) //[]byte result + err
		if err != nil {
			log.Printf("server response encode err:%v\n", err)
			return
		}

		//send result timeout
		if l.ServerOption.WriteTimeout != 0 {
			//设置写超时
			startTime = time.Now()
			conn.SetWriteDeadline(startTime.Add(l.ServerOption.WriteTimeout))
		}

		l.Plugins.BeforeWriteHook(encodeRes)
		err = l.sendData(conn, encodeRes)
		l.Plugins.AfterWriteHook(encodeRes, err)
		if err != nil {
			log.Printf("server send err:%v\n", err) //timeout
			return
		}

		log.Printf("server send result finish! total runtime: %v", time.Now().Sub(startTime).Seconds())
		return
	}
}

func (l *RPCListener) receiveData(conn net.Conn) (*protocol.RPCMsg, error) {
	l.Plugins.BeforeReadHook() //ctx

	msg, err := protocol.Read(conn)
	if err == io.EOF { //close
		log.Printf("server read finish:%v\n", err)
		return msg, nil
	}

	l.Plugins.AfterReadHook(msg, err)

	if err != nil {
		//rate limit
		return nil, err
	}
	return msg, nil
}

func (l *RPCListener) sendData(conn net.Conn, payload []byte) error {
	resMsg := protocol.NewRPCMsg()
	resMsg.SetVersion(config.Protocol_MsgVersion)
	resMsg.SetMsgType(protocol.Response)
	resMsg.SetCompressType(protocol.None)
	resMsg.SetSerializeType(protocol.Gob)
	resMsg.Payload = payload
	return resMsg.Send(conn)
}

// net addr
func (l *RPCListener) GetAddrs() []string {
	//l.nl.Addr()
	addr := fmt.Sprintf("tcp://%s:%d", l.ServiceIP, l.ServicePort)
	return []string{addr}
}

func (l *RPCListener) getDoneChan() <-chan struct{} {
	return l.doneChan
}

func (l *RPCListener) closeDoneChan() {
	select {
	case <-l.doneChan:
	default:
		close(l.doneChan)
	}
}

func (l *RPCListener) CloseConn(conn net.Conn) {
	//activeconn
	conn.Close()

	//plugin
	log.Println("server closed")
}

func (l *RPCListener) Close() {
	if l.nl != nil {
		l.nl.Close()
	}
	l.closeDoneChan()
}

func (l *RPCListener) Shutdown() {
	atomic.CompareAndSwapInt32(&l.shutdown, 0, 1)
	for {
		// 确保所有请求连接都处理完毕
		if atomic.LoadInt32(&l.handlingNum) == 0 {
			break
		}
	}
	l.closeDoneChan()
	log.Println("server shutdown")
}

// 是否处于关闭流程
func (l *RPCListener) isShutdown() bool {
	return atomic.LoadInt32(&l.shutdown) == 1
}
