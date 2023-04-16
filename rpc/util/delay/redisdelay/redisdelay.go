package redisdelay

import (
	"encoding/json"
	"errors"
	"log"
	"rpc_service/delay/redisclient"
	"time"
)

/*
Implementation of delayed scheduling by sorting linked list algorithm
排序链表算法
zadd添加元素，score设为延时任务执行时间戳（当前时间+延时时间），值为id
*/
// define bucket ticker
type BucketTicker struct {
	Ticker       *time.Ticker
	Interval     time.Duration
	Name         string
	CallbackFunc func(...interface{}) bool
}

// define task
type Task struct {
	Id        string        //task id global uniqueness
	Data      interface{}   //data of task
	Delay     time.Duration //delay time, 30 means after 30 second
	Timestamp int
}

// new ticker
func NewRedisDelay(interval time.Duration, bucketName string, callbackFunc func(...interface{}) bool) (*BucketTicker, error) {
	if interval <= 0 || callbackFunc == nil {
		return nil, errors.New("create bucket ticker instance fail")
	}
	bucket := &BucketTicker{
		Interval:     interval,
		Name:         bucketName,
		CallbackFunc: callbackFunc,
	}
	return bucket, nil
}

// add task
func (bucket *BucketTicker) AddTask(task *Task) error {
	//task id and delay time in redis zset
	timestamp := time.Now().Add(task.Delay).Unix()
	err := redisclient.ZAdd(bucket.Name, int(timestamp), task.Id)
	if err != nil {
		return err
	}
	//task body in redis string
	data, err := json.Marshal(task)
	if err != nil {
		return err
	}
	err = redisclient.Set(task.Id, string(data))
	if err != nil {
		return err
	}
	return nil
}

// start ticker
func (bucket *BucketTicker) Start() {
	timer := time.NewTicker(bucket.Interval) //interval
	go func() {
		for {
			select {
			case t := <-timer.C:
				log.Println("1 tick")
				bucket.tickHandler(t, bucket.Name)
			}
		}
	}()
}

// tick handler , The longer the interval, the lower the query frequency with Redis, but the lower the processing accuracy of delayed tasks.
func (bucket *BucketTicker) tickHandler(currentTime time.Time, bucketName string) {
	for {
		task, err := getTask(bucketName)
		if err != nil {
			log.Println("error happen!", err)
			return
		}
		if task == nil { //no task
			return
		}
		//not arrival execution time
		if task.Timestamp > int(currentTime.Unix()) {
			return
		}
		//do task
		taskDetail, err := getTaskDetail(task.Id)
		if err != nil { //retry
			log.Println("error happen!", err)
			continue
		}
		//if callback success, remove finish task
		if ok := bucket.CallbackFunc(taskDetail.Data); ok {
			err = removeTask(bucketName, task.Id)
			if err != nil {
				continue
			}
		} else {
			log.Println("error happen!", errors.New("callback error"))
			continue //retry
		}
		return
	}
}

// get task from redis zset
func getTask(bucketName string) (*Task, error) {
	value, err := redisclient.ZRangeFirst(bucketName) //ZRANGE key 0 0 WITHSCORES
	if err != nil {
		return nil, err
	}
	if value == nil {
		return nil, nil
	}
	timestamp := int(value[0].(float64))
	taskId := value[1].(string)
	task := Task{
		Id:        taskId,
		Timestamp: timestamp,
	}
	return &task, nil
}

// get task detail by taskId
func getTaskDetail(taskId string) (*Task, error) {
	v, err := redisclient.Get(taskId)
	if err != nil {
		return nil, err
	}
	if v == "" {
		return nil, nil
	}
	task := Task{}
	err = json.Unmarshal([]byte(v), &task)
	if err != nil {
		return nil, err
	}
	return &task, nil
}

// remove the task
func removeTask(bucketName string, taskId string) error {
	err := redisclient.ZRem(bucketName, taskId)
	if err != nil {
		return err
	}
	err = redisclient.Del(taskId)
	if err != nil {
		return err
	}
	return nil
}
