package timewheel

import (
	"container/list"
	"errors"
	"log"
	"time"
)

/*
Delayed tasks based on time wheel algorithm
基于时间轮算法的延时任务
*/
//define time wheel struct
type TimeWheel struct {
	ticker       *time.Ticker              //ticker
	interval     time.Duration             //time duration of moving one slot.
	buckets      []*list.List              //bucket list  Non-thread safety
	bucketSize   int                       //total size of bucket
	currentPos   int                       //current position in buckets
	callbackFunc func(...interface{}) bool //execute func
	stopChannel  chan bool                 //stop the ticker channel
}

// define task
type Task struct {
	Id     interface{}   //task id global uniqueness
	Data   interface{}   //data of task
	Delay  time.Duration //delay time, 30 means after 30 second
	Circle int           //task position in timewheel
}

// create timewheel instance
func NewTimeWheel(interval time.Duration, bucketSize int, callbackFunc func(...interface{}) bool) (*TimeWheel, error) {
	if interval <= 0 || bucketSize <= 0 || callbackFunc == nil {
		return nil, errors.New("create timewheel instance fail")
	}
	tw := &TimeWheel{
		interval:     interval,
		buckets:      make([]*list.List, bucketSize),
		bucketSize:   bucketSize,
		currentPos:   0,
		callbackFunc: callbackFunc,
		stopChannel:  make(chan bool),
	}
	//init bucket,every bucket will have a list
	for i := 0; i < bucketSize; i++ {
		tw.buckets[i] = list.New()
	}
	return tw, nil
}

// add task
func (tw *TimeWheel) AddTask(task *Task) {
	delaySeconds := int(task.Delay.Seconds())
	intervalSeconds := int(tw.interval.Seconds())
	circle := int(delaySeconds / intervalSeconds / tw.bucketSize)
	pos := int(tw.currentPos+delaySeconds/intervalSeconds) % tw.bucketSize
	task.Circle = circle
	tw.buckets[pos].PushBack(task)
}

// remove task
func (tw *TimeWheel) RemoveTask(task *Task) error {
	delaySeconds := int(task.Delay.Seconds())
	intervalSeconds := int(tw.interval.Seconds())
	circle := int(delaySeconds / intervalSeconds / tw.bucketSize)
	pos := int(tw.currentPos+delaySeconds/intervalSeconds) % tw.bucketSize
	task.Circle = circle
	t := tw.buckets[pos]

	for i := t.Front(); i != nil; i = i.Next() {
		if i.Value.(*Task).Id == task.Id {
			next := i.Next()
			t.Remove(i)
			i = next
			return nil
		}
	}
	return errors.New("no such task exist")
}

// start timewheel
func (tw *TimeWheel) Start() {
	//add ticker
	tw.ticker = time.NewTicker(tw.interval)

	//receive chan
	go func() {
		for {
			select {
			case <-tw.ticker.C: //reach a tick
				log.Println("1 tick")
				tw.tickHandler()
			case <-tw.stopChannel: //true
				tw.ticker.Stop() //stop the ticker
				return
			}
		}
	}()
}

// 1 tick handler
func (tw *TimeWheel) tickHandler() {
	bucket := tw.buckets[tw.currentPos]
	for e := bucket.Front(); e != nil; {
		task := e.Value.(*Task) //e.value is a task
		if task.Circle > 0 {
			task.Circle--
			e = e.Next()
			continue
		}
		//do task
		go func() {

			for try := 0; try <= 3; try++ {
				//if callback success, remove finish task
				if ok := tw.callbackFunc(task.Data); ok {
					err := tw.RemoveTask(task)
					if err != nil {
						log.Println("error happen! ", errors.New("remove error"))
						continue //retry
					}
				} else {
					log.Println("error happen! ", errors.New("callback error"))
					continue //retry
				}
				return
			}
			return
		}()

		//remove e
		next := e.Next()
		bucket.Remove(e)
		e = next
	}
	//finish 1 circle,reset
	if tw.currentPos == tw.bucketSize-1 {
		log.Println("new circle")
		tw.currentPos = 0
	} else {
		tw.currentPos++
	}

}

// stop timewheel
func (tw *TimeWheel) Stop() {
	tw.stopChannel <- true
}
