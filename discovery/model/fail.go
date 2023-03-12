package model

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"service_discovery/configs"
	"service_discovery/pkg/errcode"
	"service_discovery/pkg/httputil"
	"service_discovery/pkg/queue"
	"sync/atomic"
)

// 全局失败异步重试队列
// 全局 List 里装重试任务队列queue  支持并发
var gobalfailList *failList

type failList struct {
	list *queue.Queue
	size int32
}

// Failback   失败自动恢复 策略需要的参数
type ASyncPamars struct {
	id     int
	params map[string]interface{}
	//func Call() pamars
	ctx            context.Context
	uri            string
	action         configs.Action
	instance       *Instance
	data           interface{}
	failBackFail   chan error    // 异步执行成功,存放结果
	failBackSucess chan struct{} // 异步执行失败
	failBackId     chan int      // 任务ID
	isTimeOut      bool          // 判断任务是否超时
}

// 保存参数
func NewPamars(id int, ctx context.Context, uri string, action configs.Action, instance *Instance, data interface{}, params map[string]interface{}) *ASyncPamars {
	return &ASyncPamars{
		id:       id,
		ctx:      ctx,
		uri:      uri,
		action:   action,
		instance: instance,
		data:     data,
		params:   params,
	}
}

func init() {
	gobalfailList = &failList{
		list: queue.NewQueue(),
		size: 0,
	}
}

// 异步执行失败队列    Failback  策略
func AsyncFailBcak(list *failList) {
	que := list.list.Remove().(*queue.Queue)
	atomic.AddInt32(&list.size, -1) //大小减小
	len := que.Length()
	for i := 0; i < len; i++ {
		q, ok := que.Remove().(*ASyncPamars)
		if !ok {
			continue
		}
		//任务超时
		if q.isTimeOut {
			continue
		}

		go func() error {
			resp, err := httputil.HttpPost(q.uri, q.params)
			if err != nil {
				log.Println(err, "retry HttpPost")
				q.failBackFail <- err
				return err
			}
			res := Response{}
			err = json.Unmarshal([]byte(resp), &res)
			if err != nil {
				log.Println(err, "retry Unmarshal")
				q.failBackFail <- err
				return err

			}
			if res.Code != configs.StatusOK { //code!=200
				log.Printf("uri is (%v),response code (%v)\n", q.uri, res.Code)
				json.Unmarshal([]byte(res.Data), q.data)
				q.failBackFail <- fmt.Errorf("uri is (%v),response code (%v)\n", q.uri, res.Code)
				return errcode.Conflict
			}
			q.failBackId <- q.id
			q.failBackSucess <- struct{}{}
			return nil
		}()

	}
}
