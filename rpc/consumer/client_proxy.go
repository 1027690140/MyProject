package consumer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	lb "rpc_service/dispatcher/loadbalance"
	"rpc_service/naming"
	"rpc_service/protocol"
	"rpc_service/provider"
	"rpc_service/trace"
	queue "rpc_service/util"
	uid "rpc_service/util/uniqueId"
	uniqueid "rpc_service/util/uniqueId"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Shopify/sarama"
	"golang.org/x/sync/singleflight"
)

type ClientProxy interface {
	Call(context.Context, string, interface{}, ...interface{}) (interface{}, error)
}

// RPCClientProxy 代理客户端，完成连接调用，实现长连接管理、超时、重试、失败策略、鉴权(TODO)....
type RPCClientProxy struct {
	ClientOption      ClientOption
	failMode          FailMode
	registry          naming.Registry
	mutex             sync.RWMutex
	loadBalance       LoadBalance
	CommunicateMethod string // tcp or http or quic(todo)
	servers           []string
	resultChan        chan protocol.RPCMsg //回调结果
	client            Client
	httpclient        *HttpClient
	AuthConfig        provider.AuthConfig //鉴权配置

	requestGroup singleflight.Group
}

// NewClientProxy if new a httpclient new add address params
func NewClientProxy(appId string, ClientOption ClientOption, registry naming.Registry, choice interface{}, addr string) ClientProxy {
	cp := &RPCClientProxy{
		ClientOption: ClientOption,
		failMode:     ClientOption.FailMode,
		registry:     registry,
		resultChan:   make(chan protocol.RPCMsg, 1000),
		AuthConfig:   *ClientOption.AuthConfig,
	}
	servers, err := cp.discoveryService(context.Background(), appId)
	if err != nil {
		log.Fatal(err)
	}
	cp.servers = servers
	cp.loadBalance, _ = LoadBalanceFactory(ClientOption.LoadBalanceMode, cp.servers, choice)

	if ClientOption.NetProtocol == "tcp" {
		cp.client = NewClient(cp.ClientOption)
	}
	if ClientOption.NetProtocol == "http" {
		cp.httpclient, _ = DialHTTP("tcp", addr)
		//watch server:if server addrs change, update loadBalance
	}
	if ClientOption.NetProtocol == "quic" {
		//TODO
	}

	return cp
}

func (cp *RPCClientProxy) SetJWTToken(token string) {
	cp.AuthConfig.Token = token
}

// mainly for tcp
func (cp *RPCClientProxy) Call(ctx context.Context, servicePath string, stub interface{}, params ...interface{}) (interface{}, error) {
	service, err := NewService(servicePath)
	if err != nil {
		return nil, err
	}

	err = cp.getTcpConn()
	if err != nil && cp.failMode == Failfast {
		log.Println("failfast:", err)
		return nil, err
	}
	// 创建带有TraceInfo的context.Context对象
	TraceID, _ := uniqueid.ULIDQueue.Pop()
	// 初始TraceInfo
	traceinfo := &trace.TraceInfo{
		TraceID:  TraceID,
		ParentID: service.AppId,
		ID:       service.AppId,
		ServiceMetadata: &trace.ServiceMetadata{
			Instance: &naming.Instance{
				Env:      service.Env,
				Hostname: service.Hostname,
				Zone:     service.Zone,
				Version:  service.Version,
				AppID:    service.AppId,
				Status:   service.Status,
			}, // 填入实例信息，如果没有实例信息，可以设置为nil
			Metadata: make(map[string]string), // 填入具体的元数据信息
		},
		Timestamp: time.Now().Unix(),
		Kind:      "Client",
		Tags: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
		RemoteEndpoint: &trace.Endpoint{
			ServiceName: service.AppId,
			Hostname:    service.Hostname,
		},
		Debug:  false, // 设置为true以启用调试模式，如果不需要可以设置为false
		Shared: false, // 设置为true以共享Span，如果不需要可以设置为false
		Error: []*trace.ErrorInfo{
			&trace.ErrorInfo{},
		},
		Events: []*trace.Event{
			{
				Timestamp: time.Now().UnixNano(),
				Name:      "Client_Send",
				Data:      []interface{}{"servicePath" + servicePath, params},
			},
		},
	}
	ctx = context.WithValue(context.Background(), "trace_info", traceinfo)

	//失败策略
	switch cp.failMode {
	//失败重试
	case Failretry:
		retries := cp.ClientOption.Retries
		if cp.client == nil {
			return nil, errors.New("call error")
		}
		for retries > 0 {
			retries--
			// 多次retry  singlefly
			rs := cp.requestGroup.DoChan(servicePath, func() (interface{}, error) {

				if retries > 10 {
					// 另外启用协程定时删除key，提高请求下游次数，提高成功率
					go func() {
						time.Sleep(100 * time.Millisecond)
						cp.requestGroup.Forget(servicePath)
					}()
				}
				return cp.client.Invoke(ctx, service, stub, params...)
			})
			select {
			case <-ctx.Done():

				eif := &trace.ErrorInfo{
					Code:    strconv.Itoa(4),
					Message: fmt.Sprint("--Failretry -- servicePath : %s  ime out ", servicePath),
				}
				traceinfo.Error = append(traceinfo.Error, eif)
				Event := &trace.Event{
					Timestamp: time.Now().UnixNano(),
					Name:      "Failretry falil",
					Data:      []interface{}{servicePath, cp.client.GetAddr(), err},
				}
				traceinfo.Events = append(traceinfo.Events, Event)
				traceinfo.EditTimestamp = time.Now().UnixNano()

				go trace.ArchiveAndPersistTraceInfo(traceinfo)
				return nil, fmt.Errorf("time out")
			case res := <-rs:
				go trace.ArchiveAndPersistTraceInfo(traceinfo)
				return res, nil
			default:
				time.Sleep(500 * time.Microsecond)
				continue
			}

		}
		//Failover 失败转移
	case Failover:
		traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)

		retries := cp.ClientOption.Retries
		for retries > 0 {
			retries--
			if cp.client != nil {
				traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)
				rs, err := cp.client.Invoke(ctx, service, stub, params...)
				//err == ok
				if err == nil || err.Code_() == 0 {
					go trace.ArchiveAndPersistTraceInfo(traceinfo)

					return rs, nil
				}
			}
			// 重试失败，切换server  getTcpConn 内置了负载均衡，自动切换server adrr
			err = cp.getTcpConn()
			eif := &trace.ErrorInfo{
				Code:    strconv.Itoa(23),
				Message: fmt.Sprint("--fail getTcpConn--", cp.client.GetAddr()),
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			traceinfo.EditTimestamp = time.Now().UnixNano()

			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			log.Println("--failover new server--", cp.client.GetAddr())
		}
		//快速失败
	case Failfast:
		traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)
		if cp.client != nil {
			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				go trace.ArchiveAndPersistTraceInfo(traceinfo)
				return rs, nil
			}
			eif := &trace.ErrorInfo{
				Code:    strconv.Itoa(err.Code_()),
				Message: fmt.Sprint("--Failfast -- servicePath: %s  ", servicePath) + err.Message,
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			traceinfo.EditTimestamp = time.Now().UnixNano()

			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			return nil, err
		}
		//失败安全
	case Failsafe:
		if cp.client != nil {

			traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)
			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				// 调用成功，将链路信息归档和持久化
				go trace.ArchiveAndPersistTraceInfo(traceinfo)
				return rs, nil
			}

			// 调用失败，记录错误信息到traceinfo.Error
			eif := &trace.ErrorInfo{
				Code:    strconv.Itoa(err.Code_()),
				Message: fmt.Sprint("--Failsafe -- servicePath: %s  ", servicePath) + err.Message,
			}
			traceinfo.Error = append(traceinfo.Error, eif)
			traceinfo.EditTimestamp = time.Now().UnixNano()

			go trace.ArchiveAndPersistTraceInfo(traceinfo)
			log.Printf("--Failsafe -- servicePath%s", servicePath)

			return nil, err
		}

		//Failback
	case Failback:
		traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)
		if cp.client != nil {
			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				// 调用成功，将链路信息归档和持久化
				go trace.ArchiveAndPersistTraceInfo(traceinfo)
				return rs, nil
			}
			log.Printf("--Failback -- servicePath ")
			//执行失败，开始Failback
			eif := &trace.ErrorInfo{
				Code:    strconv.Itoa(err.Code_()),
				Message: fmt.Sprint("--Failback -- servicePath ", servicePath) + err.Message,
			}
			traceinfo.Error = append(traceinfo.Error, eif)

			Event := &trace.Event{
				Timestamp: time.Now().UnixNano(),
				Name:      "Failback",
				Data:      []interface{}{servicePath, cp.client.GetAddr(), err, params},
			}
			traceinfo.Events = append(traceinfo.Events, Event)
			traceinfo.EditTimestamp = time.Now().UnixNano()

			go trace.ArchiveAndPersistTraceInfo(traceinfo)

			// 生产全局ID
			id := uid.GenerateSnowflakeID()
			// 生成参数
			failPamars := NewFailBackPamars(ctx, cp, id, service, stub, params...)
			// 加入失败队列
			if atomic.LoadInt32(&gobalfailList.size) == 0 { //队列为空
				q := queue.NewQueue()
				q.Add(failPamars)
				gobalfailList.list.Add(q)
				atomic.AddInt32(&gobalfailList.size, 1)
			} else {
				p := gobalfailList.list.Peek().(*queue.Queue)
				p.Add(failPamars)
			}

			//异步执行
			go SyncFailBcak(gobalfailList)

			// if 不需要返回值，直接 return

			//等待执行结果
			select {

			//异步执行成功
			case rs := <-failPamars.failBackSucess:
				idd := <-failPamars.failBackID
				if failPamars.id == idd { //确保id对应
					Event := &trace.Event{
						Timestamp: time.Now().UnixNano(),
						Name:      "Failback Sucess",
					}
					traceinfo.Events = append(traceinfo.Events, Event)
					traceinfo.EditTimestamp = time.Now().UnixNano()

					go trace.ArchiveAndPersistTraceInfo(traceinfo)
					return rs, nil
				}
				log.Printf("--Failback -- servicePath --- task ID mismatchr ")
				return nil, errors.New("task ID mismatch")

			//异步执行失败
			case err = <-failPamars.failBackFail:
				// 调用失败，记录错误信息到traceinfo.Error
				eif := &trace.ErrorInfo{
					Code:    strconv.Itoa(err.Code_()),
					Message: fmt.Sprint("--Failback -- servicePath ", servicePath) + err.Message,
				}
				traceinfo.Error = append(traceinfo.Error, eif)

				Event := &trace.Event{
					Timestamp: time.Now().UnixNano(),
					Name:      "Failback fail",
					Data:      []interface{}{servicePath, cp.client.GetAddr(), err, params},
				}
				traceinfo.Events = append(traceinfo.Events, Event)
				traceinfo.EditTimestamp = time.Now().UnixNano()
				go trace.ArchiveAndPersistTraceInfo(traceinfo)

				log.Printf("--Failback -- servicePath ---again fail ")
				return nil, err
			//异步执行超时
			case <-ctx.Done():
				// 设置超时 逻辑删除，队列中的任务不执行  Set timeout tombstone, task  do not execute
				failPamars.isTimeOut = true
				log.Printf("--Failback -- servicePath ---time over ")
				eif := &trace.ErrorInfo{
					Code:    strconv.Itoa(4),
					Message: fmt.Sprint("--Failback -- servicePath time over  ", servicePath) + err.Message,
				}
				traceinfo.Error = append(traceinfo.Error, eif)

				Event := &trace.Event{
					Timestamp: time.Now().UnixNano(),
					Name:      "Failback fail time over",
					Data:      []interface{}{servicePath, cp.client.GetAddr(), err, params},
				}
				traceinfo.Events = append(traceinfo.Events, Event)
				traceinfo.EditTimestamp = time.Now().UnixNano()
				go trace.ArchiveAndPersistTraceInfo(traceinfo)

				return nil, ctx.Err()
			}
		}
	}

	traceinfo, _ = trace.GetLatestTraceInfoFromRedis(TraceID)
	//跨span调用
	if traceinfo.ID != service.AppId {
		go trace.ArchiveAndPersistTraceInfo(traceinfo)
		traceinfo.ParentID = traceinfo.ID
		traceinfo.ID = service.AppId
		traceinfo.Annotations = append(traceinfo.Annotations, &trace.Annotation{Value: l.ServerOption.AppID, Timestamp: time.Now().Unix()})
	}
	eif := &trace.ErrorInfo{
		Code:    strconv.Itoa(24),
		Message: fmt.Sprint(" --client call error  ", servicePath),
	}
	traceinfo.Error = append(traceinfo.Error, eif)
	traceinfo.EditTimestamp = time.Now().UnixNano()
	go trace.ArchiveAndPersistTraceInfo(traceinfo)
	return nil, errors.New("call error")
}

// TCP
func (cp *RPCClientProxy) getTcpConn() error {

	var s string
	var err error

	//负载均衡 如果是一致性哈希，需要传入key
	if cp.ClientOption.LoadBalanceMode == 4 {
		s = cp.loadBalance.(*lb.HashBalancer).GetByKey(cp.servers[rand.Int()])
	} else {
		s, err = cp.loadBalance.Get()
	}

	addr := strings.Replace(s, cp.ClientOption.NetProtocol+"://", "", -1)

	if err == nil {
		fmt.Errorf("get balancer fail")
	}
	err = cp.client.Connect(addr) //长连接管理
	if err != nil {
		log.Println("connect server fail:", err)
		return err
	}
	log.Println("connect server:" + addr)
	return nil
}

func (cp *RPCClientProxy) discoveryService(ctx context.Context, appId string) ([]string, error) {
	instances, ok := cp.registry.Fetch(ctx, appId)
	if !ok {
		return nil, errors.New("service not found")
	}
	var servers []string
	for _, instance := range instances {
		servers = append(servers, instance.Addrs...)
	}
	log.Println(appId, " found service addrs: ", servers)
	return servers, nil
}

// 动态配置更新，开启协程定时更新severs[]
func updateServices()

// 处理kafka消息
func (cp *RPCClientProxy) HandleKafkaMessage(msg *sarama.ConsumerMessage) {
	// 解析RPC响应
	var response protocol.RPCMsg
	err := json.Unmarshal(msg.Value, &response)
	if err != nil {
		log.Println("Failed to unmarshal Kafka message:", err)
		return
	}

	// 在这里处理收到的 Kafka 消息，并执行相应的逻辑
	// 调用 Call 函数或执行其他操作
	// 示例中的代码将收到的消息传递给 Call 函数
	servicePath := response.ServiceAppID + "." + response.ServiceClass + "." + response.ServiceMethod
	_, err = cp.Call(context.Background(), servicePath, nil, response.Payload)
	if err != nil {
		log.Println("Failed to call RPC:", err)
	}
}

func (cp *RPCClientProxy) GetResultFromRPCClientProxy() (interface{}, error) {
	// 设置超时时间
	timeout := time.After(RequestTimeout)

	// 循环等待获取结果或超时
	for {
		select {
		case <-timeout:
			return nil, errors.New("RPC request timed out")
		case result := <-cp.resultChan:
			if result.Error != nil {
				// 处理错误
				return nil, result.Error
			}
			return result.Payload, nil
		}
	}
}
