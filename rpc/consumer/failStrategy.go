package consumer

import (
	"context"
	"rpc_service/global"
	queue "rpc_service/util"
	"sync"
	"sync/atomic"
)

type FailMode int

const (
	Failover  FailMode = iota //快速转移		也就是故障转移，换个服务端实例再试
	Failfast                  //快速失败
	Failback                  //失败自动恢复	 如果调用失败，则此次失败相当于Failsafe，将返回一个空结果。而与Failsafe不同的是，Failback策略会将这次调用加入内存中的失败列表中，对于这个列表中的失败调用，会在另一个线程中进行异步重试，重试如果再发生失败，则会忽略，即使重试调用成功，原来的调用方也感知不到了。因此它通常适合于，对于实时性要求不高，且不需要返回值的一些异步操作。
	Failretry                 //失败重试
	Failsafe                  //失败安全		应用场景，可以用于写入审计日志等操作。
)

// FailBackPamars   失败自动恢复 策略需要的参数
type FailBackPamars struct {
	id int

	cp *RPCClientProxy

	//func Call() pamars
	ctx            context.Context
	service        *Service
	stub           interface{}
	params         []interface{}
	failBackFail   chan *global.StatusError // 异步执行成功,存放结果
	failBackID     chan int                 // 任务ID
	failBackSucess chan interface{}         // 异步执行失败
	isTimeOut      bool                     // 判断任务是否超时
}

// NewFailBackPamars 保存参数
func NewFailBackPamars(ctx context.Context, cp *RPCClientProxy, id int, service *Service, stub interface{}, params ...interface{}) *FailBackPamars {
	return &FailBackPamars{
		id:             id,
		cp:             cp,
		ctx:            ctx,
		service:        service,
		stub:           stub,
		params:         params,
		failBackFail:   make(chan *global.StatusError, 1),
		failBackID:     make(chan int, 1),
		failBackSucess: make(chan interface{}, 1),
		isTimeOut:      false,
	}
}

// 全局失败异步重试队列    Failback  策略
// 全局 List 里装重试任务队列queue  支持并发
var gobalfailList *failList

type failList struct {
	list *queue.Queue
	size int32
	lock sync.RWMutex
}

func init() {
	gobalfailList = &failList{
		list: queue.NewQueue(),
		size: 0,
	}

}

// SyncFailBcak 异步执行失败队列    Failback  策略
func SyncFailBcak(list *failList) {
	list.lock.RLock()
	que := list.list.Remove().(*queue.Queue)
	atomic.AddInt32(&list.size, -1) //大小减小
	len := que.Length()
	list.lock.RUnlock()
	for i := 0; i < len; i++ {
		q, ok := que.Remove().(*FailBackPamars)
		if !ok {
			continue
		}
		//任务超时
		if q.isTimeOut {
			continue
		}

		go func() {
			res, err := q.cp.client.Invoke(q.ctx, q.service, q.stub, q.params...)
			if err == nil {
				// 返回id
				q.failBackID <- q.id
				// 返回结果
				q.failBackSucess <- res
			}
			//通知失败
			q.failBackFail <- err
		}()
	}
}
