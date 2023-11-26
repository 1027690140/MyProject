package provider

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"rpc_service/naming"
	"rpc_service/trace"
	"strings"
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
	AuthService  AuthService
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

		// Read authentication credentials from the client 进行用户安全认证的逻辑处理
		credentials, err := l.readCredentials(conn)
		if err != nil {
			log.Printf("server authentication error: %v", err)
			return
		}
		// Authenticate the client using the credentials
		err = l.AuthService.Intercept(credentials)
		if err != nil {
			log.Printf("server authentication failed: %v", err)
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

		//traceinfo
		traceID := string(msg.Header[5])
		traceinfo, err2 := trace.GetLatestTraceInfoFromRedis(traceID)
		traceinfo.EditTimestamp = time.Now().Unix()
		if err2 != nil {
			log.Println("server get trace info error:", err2.Message)
		}
		//跨span调用
		if traceinfo.ID != l.ServerOption.AppID {
			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			traceinfo.ParentID = traceinfo.ID
			traceinfo.ID = l.ServerOption.AppID
			traceinfo.Annotations = append(traceinfo.Annotations, &trace.Annotation{Value: l.ServerOption.AppID, Timestamp: time.Now().Unix()})
		}
		traceinfo.RemoteEndpoint.Port = l.ServicePort
		traceinfo.RemoteEndpoint.IPv6 = l.ServiceIP
		traceinfo.RemoteEndpoint.ServiceName = msg.ServiceClass
		traceinfo.RemoteEndpoint.Hostname = l.ServerOption.Hostname
		traceinfo.Events = append(traceinfo.Events, &trace.Event{Name: "server run", Timestamp: time.Now().Unix(), Data: []interface{}{msg}})
		traceinfo.ServiceMetadata = &trace.ServiceMetadata{
			Instance: &naming.Instance{
				Zone:     l.ServerOption.Zone,
				Env:      l.ServerOption.Env,
				AppID:    l.ServerOption.AppID,
				Hostname: l.ServerOption.Hostname,
				Addrs:    []string{fmt.Sprintf("%s:%d", l.ServiceIP, l.ServicePort)},
			},
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
			traceinfo.Error = append(traceinfo.Error, &trace.ErrorInfo{Message: fmt.Sprintf("server can not found handler error: %s", msg.ServiceClass)})
			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			return
		}

		l.Plugins.BeforeCallHook(msg.ServiceClass, msg.ServiceMethod, inArgs)

		result, err := handler.Handle(msg.ServiceMethod, inArgs)

		traceinfo.Events = append(traceinfo.Events, &trace.Event{Name: fmt.Sprintf("server callmethod %s ", msg.ServiceMethod), Timestamp: time.Now().Unix(), Data: []interface{}{result, err}})

		l.Plugins.AfterCallHook(msg.ServiceClass, msg.ServiceMethod, inArgs, result, err)

		//encode
		encodeRes, err := coder.Encode(result) //[]byte result + err
		if err != nil {
			log.Printf("server response encode err:%v\n", err)
			traceinfo.Error = append(traceinfo.Error, &trace.ErrorInfo{Code: "encode err", Message: fmt.Sprintf("server response encode err:%v\n", err)})
			go trace.ArchiveAndPersistTraceInfo(traceinfo)

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
			traceinfo.Error = append(traceinfo.Error, &trace.ErrorInfo{Code: "server send err", Message: fmt.Sprintf("server send err:%v\n", err)})
			go trace.ArchiveAndPersistTraceInfo(traceinfo)

			return
		}

		log.Printf("server send result finish! total runtime: %v", time.Now().Sub(startTime).Seconds())
		go trace.ArchiveAndPersistTraceInfo(traceinfo)
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

// 读取认证凭据
func (l *RPCListener) readCredentials(conn net.Conn) (Credentials, error) {

	// 示例：从 RPCMsg 的 Metadata 中获取 Authorization 头部
	rpcMsg, err := l.receiveData(conn)
	if err != nil {
		return Credentials{}, err
	}

	authHeader := rpcMsg.Metadata["Authorization"]
	if authHeader == "" {
		return Credentials{}, errors.New("身份验证失败：缺少 Authorization 头部")
	}

	// 示例：解析 Authorization 头部的凭据
	decodedCreds, err := base64.StdEncoding.DecodeString(authHeader)
	if err != nil {
		return Credentials{}, errors.New("身份验证失败：无效的 Authorization 头部")
	}

	// 示例：将凭据拆分为用户名和密码
	creds := strings.SplitN(string(decodedCreds), ":", 2)
	if len(creds) != 2 {
		return Credentials{}, errors.New("身份验证失败：无效的 Authorization 头部")
	}

	// 构建 AuthConfig 结构体并返回
	authConfig := AuthConfig{
		Username: creds[0],
		Password: creds[1],
		// 添加其他必要字段的初始化
	}

	credentials := Credentials{
		AuthConfig: &authConfig,
		ExpiryTime: time.Now().Add(time.Hour), // 设置一个示例的过期时间
		// 添加其他必要字段的初始化
	}

	return credentials, nil
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
