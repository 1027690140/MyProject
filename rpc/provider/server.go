package provider

import (
	"context"
	"errors"
	"log"
	"reflect"
	"rpc_service/naming"
	"rpc_service/protocol"

	"sync"

	"time"
)

var MagicNumber byte = 0x06

var maxRegisterRetry int = 2

// Server is RPC 服务端接口
type Server interface {
	Register(string, interface{}) //error
	Run()
	Close()
	Shutdown()
}

type ServerOption struct {
	MagicNumber       byte
	Ip                string
	Port              int
	Hostname          string
	AppId             string
	Env               string
	NetProtocol       string
	ReadTimeout       time.Duration
	WriteTimeout      time.Duration
	HandleTimeout     time.Duration
	ConnectionTimeout time.Duration
	SerializeType     protocol.SerializeType
	CompressType      protocol.CompressType
}

var DefaultServerOption = ServerOption{
	MagicNumber:   MagicNumber,
	NetProtocol:   "tcp",
	ReadTimeout:   5 * time.Second,
	WriteTimeout:  5 * time.Second,
	HandleTimeout: 10 * time.Second,
	SerializeType: protocol.Gob,
	CompressType:  protocol.None,
}

// TCP Server
type RPCServer struct {
	engine       *Engine            // use for http router
	listener     Listener           // Used to bind the service instance.
	registry     naming.Registry    // Used to bind the service registry instance.
	cancelFunc   context.CancelFunc // Used to cancel the service registry.
	ServerOption ServerOption       //
	Plugins      PluginContainer    //
	serviceMap   sync.Map           // record method calls
}

// NewRPCServer
func NewRPCServer(ServerOption ServerOption, registry naming.Registry) *RPCServer {
	if ServerOption.NetProtocol == "" {
		ServerOption.NetProtocol = DefaultServerOption.NetProtocol
	}
	if ServerOption.ReadTimeout <= 0 {
		ServerOption.ReadTimeout = DefaultServerOption.ReadTimeout
	}
	if ServerOption.WriteTimeout <= 0 {
		ServerOption.WriteTimeout = DefaultServerOption.WriteTimeout
	}
	if ServerOption.HandleTimeout <= 0 {
		ServerOption.HandleTimeout = DefaultServerOption.HandleTimeout
	}

	return &RPCServer{
		listener:     NewRPCListener(ServerOption),
		registry:     registry,
		ServerOption: ServerOption,
		Plugins:      &pluginContainer{},
	}
}

// Register service
func (svr *RPCServer) Register(class interface{}) {
	name := reflect.Indirect(reflect.ValueOf(class)).Type().Name()
	svr.RegisterName(name, class)
}

// RegisterName to RPCServerHandler
func (svr *RPCServer) RegisterName(name string, class interface{}) {
	handler := &RPCServerHandler{class: reflect.ValueOf(class)}
	svr.listener.SetHandler(name, handler)
	svr.Plugins.RegisterHook(name, class)
	log.Printf("%s registered success!\n", name)
}

// Run and service start  tcp
func (svr *RPCServer) Run() {
	//先启动后暴露服务
	svr.listener.SetPlugins(svr.Plugins)
	// TODO  use goroutine pool
	err := svr.listener.Run()
	if err != nil {
		panic(err)
	}

	//注册失败，重试,多次失败退出服务
	err = svr.registerToNaming()
	if err != nil {
		svr.Close()
		panic(err)
	}
}

// Close service
func (svr *RPCServer) Close() {
	log.Println("close and cancel: ", svr.ServerOption.AppId, svr.ServerOption.Hostname)
	//从服务注册中心注销
	if svr.cancelFunc != nil {
		svr.cancelFunc()
	}
	//关闭当前服务
	if svr.listener != nil {
		svr.listener.Close()
	}
}

// Shutdown   gracefully
func (svr *RPCServer) Shutdown() {
	log.Println("shutdown and cancel:", svr.ServerOption.AppId, svr.ServerOption.Hostname)
	//从服务注册中心注销
	if svr.cancelFunc != nil {
		svr.cancelFunc()
	}
	//关闭当前服务
	if svr.listener != nil {
		svr.listener.Shutdown()
	}
}

func (svr *RPCServer) registerToNaming() error {
	instance := &naming.Instance{
		Env:      svr.ServerOption.Env,
		AppId:    svr.ServerOption.AppId,
		Hostname: svr.ServerOption.Hostname,
		Addrs:    svr.listener.GetAddrs(),
	}
	retries := maxRegisterRetry
	for retries > 0 {
		retries--
		cancel, err := svr.registry.Register(context.Background(), instance)
		if err == nil {
			log.Println("register to naming server success: ", svr.ServerOption.AppId, svr.ServerOption.Hostname)
			svr.cancelFunc = cancel
			return nil
		}
	}
	return errors.New("register to naming server fail")
}
