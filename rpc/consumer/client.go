package consumer

import (
	"context"
	//"errors"
	//"fmt"
	"log"
	"net"
	"reflect"
	"rpc_service/config"
	"rpc_service/global"
	"rpc_service/protocol"
	"time"
)

type Client interface {
	Connect(string) error
	//直接执行
	Invoke(context.Context, *Service, interface{}, ...interface{}) (interface{}, error)
	//构造执行方法
	MakeFunc(*Service, interface{})
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
	NetProtocol       string //"tcp", "udp","ip", "ip4", "ip6","unix", "unixgram", "unixpacket";tcp or http or quic(todo)
	LoadBalanceMode   LoadBalanceMode
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
}

var _ Client = new(RPCClient)

// TCP client
type RPCClient struct {
	conn         net.Conn
	ClientOption ClientOption
	addr         string
}

func NewClient(ClientOption ClientOption) Client {
	return &RPCClient{ClientOption: ClientOption}
}

// "tcp", "tcp4", "tcp6":
func (cli *RPCClient) Connect(addr string) error {
	conn, err := net.DialTimeout(cli.ClientOption.NetProtocol, addr, cli.ClientOption.ConnectionTimeout)
	if err != nil {
		return err
	}
	cli.conn = conn
	cli.addr = addr
	return nil
}

func (cli *RPCClient) Invoke(ctx context.Context, service *Service, stub interface{}, params ...interface{}) (interface{}, error) {
	//make func : this step can be prepared before invoke and store into cache
	cli.MakeFunc(service, stub)
	//reflect call
	return cli.wrapCall(ctx, stub, params...)
}

func (cli *RPCClient) Close() {
	if cli.conn != nil {
		cli.conn.Close()
	}
}

func (cli *RPCClient) GetAddr() string {
	//cli.conn.RemoteAddr().String()
	return cli.addr
}

// make call func.
//
//	The proxy function is generated through reflection,
//	and the work of network connection,
//	request data serialization,
//	network transmission,
//	and response return data analysis is completed in the proxy function
func (cli *RPCClient) MakeFunc(service *Service, methodPtr interface{}) {
	container := reflect.ValueOf(methodPtr).Elem() //反射获取函数元素
	coder := global.Codecs[cli.ClientOption.SerializeType]

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

		//in args
		inArgs := make([]interface{}, 0, len(req))
		for _, arg := range req {
			inArgs = append(inArgs, arg.Interface())
		}

		payload, err := coder.Encode(inArgs) //[]byte
		if err != nil {
			log.Printf("encode err:%v\n", err)
			return errorHandler(err)
		}

		//send by network
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
		msg.Payload = payload

		err = msg.Send(cli.conn)
		if err != nil {
			log.Printf("send err:%v\n", err)
			return errorHandler(err)
		}
		log.Println("send success!")

		//read from network
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
		return outArgs
	}

	container.Set(reflect.MakeFunc(container.Type(), handler)) //构造函数
}

func (cli *RPCClient) wrapCall(ctx context.Context, stub interface{}, params ...interface{}) (interface{}, error) {
	f := reflect.ValueOf(stub).Elem()
	if len(params) != f.Type().NumIn() {
		return nil, global.ParamErr
	}

	in := make([]reflect.Value, len(params))
	for idx, param := range params {
		in[idx] = reflect.ValueOf(param)
	}
	result := f.Call(in)
	return result, nil
}
