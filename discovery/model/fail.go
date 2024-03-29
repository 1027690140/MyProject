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
var gobalfailmonitorList *failList // 更新配置文件失败队列

type failList struct {
	list *queue.Queue
	size int32
}

// Failback   失败自动恢复 策略需要的参数
type ASyncPamars struct {
	id int
	//func Call() pamars
	params         map[string]interface{}
	ctx            context.Context
	uri            string
	action         configs.Action
	instance       *Instance
	data           interface{}
	failBackSucess chan struct{} // 异步执行成功
	failBackFail   chan error    // 异步执行失败
	failBackId     chan int      // 任务ID
	isTimeOut      bool          // 判断任务是否超时
}

// 保存参数
func NewPamars(ctx context.Context, uri string, action configs.Action, instance *Instance, data interface{}, params map[string]interface{}) *ASyncPamars {
	return &ASyncPamars{
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

	gobalfailmonitorList = &failList{
		list: queue.NewQueue(),
		size: 0,
	}
}

// call方法 异步执行失败队列    Failback  策略
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

// 动态更新重试失败队列
func monitorRetry(list *failList) {
	que := list.list.Remove().(*queue.Queue)
	atomic.AddInt32(&list.size, -1) //大小减小
	len := que.Length()
	for i := 0; i < len; i++ {
		c, ok := que.Remove().(*configs.GlobalConfig)
		if !ok {
			continue
		}
		d, ok := que.Remove().(*Discovery)
		if !ok {
			continue
		}
		go func() error {
			return d.updateConfig(c)
		}()
	}

}
