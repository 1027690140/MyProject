package consumer

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	lb "rpc_service/dispatcher/loadbalance"
	"rpc_service/global"
	"rpc_service/naming"
	queue "rpc_service/util"
	uid "rpc_service/util/uniqueId"
	"strings"
	"sync"
	"sync/atomic"
	"time"

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

	client     Client
	httpclient *HttpClient

	requestGroup singleflight.Group
}

// NewClientProxy if new a httpclient new add address params
func NewClientProxy(appId string, ClientOption ClientOption, registry naming.Registry, choice interface{}, addr string) ClientProxy {
	cp := &RPCClientProxy{
		ClientOption: ClientOption,
		failMode:     ClientOption.FailMode,
		registry:     registry,
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
				return nil, fmt.Errorf("time out")
			case res := <-rs:
				return res, nil
			default:
				time.Sleep(500 * time.Microsecond)
				continue
			}

		}
		//Failover 失败转移
	case Failover:
		retries := cp.ClientOption.Retries
		for retries > 0 {
			retries--
			if cp.client != nil {

				rs, err := cp.client.Invoke(ctx, service, stub, params...)
				//err == global.paramErr
				if err == nil || err == global.ParamErr {
					return rs, nil
				}
			}
			// 重试失败，切换server  getTcpConn 内置了负载均衡，自动切换server adrr
			err = cp.getTcpConn()
			log.Println("--failover new server--", cp.client.GetAddr())
		}
		//快速失败
	case Failfast:
		if cp.client != nil {
			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				return rs, nil
			}
			return nil, err
		}
		//失败安全
	case Failsafe:
		if cp.client != nil {

			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				return rs, nil
			}
			log.Printf("--Failsafe -- servicePath ")
			return nil, err
		}
		//Failback
	case Failback:
		if cp.client != nil {
			rs, err := cp.client.Invoke(ctx, service, stub, params...)
			if err == nil {
				return rs, nil
			}
			log.Printf("--Failback -- servicePath ")
			//执行失败，开始Failback

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
					return rs, nil
				}
				log.Printf("--Failback -- servicePath --- task ID mismatchr ")
				return nil, errors.New("task ID mismatch")

			//异步执行失败
			case err = <-failPamars.failBackFail:
				log.Printf("--Failback -- servicePath ---again fail ")
				return nil, err
			//异步执行超时
			case <-ctx.Done():
				// 设置超时 逻辑删除，队列中的任务不执行  Set timeout tombstone, task  do not execute
				failPamars.isTimeOut = true
				log.Printf("--Failback -- servicePath ---time over ")
				return nil, ctx.Err()
			}
		}
	}

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
