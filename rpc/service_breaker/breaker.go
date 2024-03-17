package serviceBreaker

import (
	"context"
	"errors"
	"fmt"
	"log"
	queue "rpc_service/util"
	uid "rpc_service/util/uniqueId"

	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

/*
fuse
Synchronous serialization of requests, due to front and rear operations and locks,
performance is reduced, and there are concurrency problems.

熔断
请求同步串行化，由于前后置操作和锁,性能降低，存在并发问题。
*/

var (
	ErrStateOpen    = errors.New("service breaker is open")
	ErrTooManyCalls = errors.New("service breaker is halfopen, too many calls")
)

// service breaker
type ServiceBreaker struct {
	mu               sync.RWMutex                                      //rwlock
	name             string                                            //name
	state            State                                             //current state
	windowInterval   time.Duration                                     //time window interval
	metrics          Metrics                                           //time window metrics statistics
	tripStrategyFunc TripStrategyFunc                                  //determine whether the breaker should be opened
	halfMaxCalls     uint64                                            //state is halfopen,max number of calls allowed.
	sleepTimeout     time.Duration                                     //state is open, after timeout will try to call.
	stateChangeHook  func(name string, fromState State, toState State) //hook if state change
	stateOpenTime    time.Time                                         //when state is open
	failMode         FailMode
	option           Option

	requestGroup singleflight.Group
}

// new breaker
func NewServiceBreaker(op Option) (*ServiceBreaker, error) {
	if op.WindowInterval <= 0 || op.HalfMaxCalls <= 0 || op.SleepTimeout <= 0 {
		return nil, errors.New("incomplete options")
	}
	breaker := new(ServiceBreaker)
	breaker.name = op.Name
	breaker.windowInterval = op.WindowInterval
	breaker.halfMaxCalls = op.HalfMaxCalls
	breaker.sleepTimeout = op.SleepTimeout
	breaker.stateChangeHook = op.StateChangeHook
	breaker.tripStrategyFunc = ChooseTrip(&op.TripStrategy)
	breaker.nextWindow(time.Now())
	return breaker, nil
}

// use breaker to call
func (breaker *ServiceBreaker) Call(ctx context.Context, exec func() (interface{}, error)) (interface{}, error) {
	log.Printf("start call, %v state is %v\n", breaker.name, breaker.state)
	//before call
	err := breaker.beforeCall()
	if err != nil {
		log.Printf("end call,%v batch:%v,metrics:(%v,%v,%v,%v,%v),window time start:%v\n\n",
			breaker.name,
			breaker.metrics.WindowBatch,
			breaker.metrics.CountAll,
			breaker.metrics.CountSuccess,
			breaker.metrics.CountFail,
			breaker.metrics.ConsecutiveSuccess,
			breaker.metrics.ConsecutiveFail,
			breaker.metrics.WindowTimeStart.Format("2006/01/02 15:04:05"))
		return nil, err
	}

	//if panic occur
	defer func() (interface{}, error) {
		err := recover()
		if err != nil {
			breaker.afterCall(false)
			//失败策略
			switch breaker.failMode {
			//失败重试
			case Failretry:
				retries := breaker.option.Retries

				for retries > 0 {
					retries--
					// 多次retry  singlefly
					rs := breaker.requestGroup.DoChan(breaker.option.Name, func() (interface{}, error) {
						if retries > 10 {
							go func() {
								time.Sleep(100 * time.Millisecond)
								breaker.requestGroup.Forget(breaker.option.Name)
							}()
						}
						return exec()
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

				//快速失败
			case Failfast:
				rs, err := exec()
				if err == nil {
					return rs, nil
				}
				return nil, err
				//失败安全
			case Failsafe:

				rs, err := exec()
				if err == nil {
					return rs, nil
				}
				log.Printf("--Failsafe -- call ")
				return nil, err
			case Failback:
				rs, err := exec()
				if err == nil {
					return rs, nil
				}
				log.Printf("--Failback -- servicePath ")
				//执行失败，开始Failback

				// 生产全局ID
				id := uid.GenerateSnowflakeID()
				// 生成参数
				failPamars := NewFailBackPamars(id, ctx, breaker, exec)
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

				//等待执行结果
				select {

				//异步执行成功
				case rs := <-failPamars.failBackSucess:
					idd := <-failPamars.failBackId
					if failPamars.id == idd { //确保id对应
						return rs, nil
					}
					log.Printf("--breaker --- task ID mismatchr ")
					return nil, errors.New("  ID mismatch")

				//异步执行失败
				case err = <-failPamars.failBackFail:
					log.Printf("-breaker ---again fail ")
					return nil, err
				//异步执行超时
				case <-ctx.Done():
					// 设置超时 逻辑删除，队列中的任务不执行  Set timeout tombstone, task  do not execute
					failPamars.isTimeOut = true
					log.Printf("--breaker --  ---time over ")
					return nil, ctx.Err()
				}
			}
		}

		return nil, nil
	}()

	//call
	breaker.metrics.OnCall()
	result, err := exec()

	//after call
	breaker.afterCall(err == nil)
	log.Printf("end call,%v batch:%v,metrics:(%v,%v,%v,%v,%v),window time start:%v\n\n",
		breaker.name,
		breaker.metrics.WindowBatch,
		breaker.metrics.CountAll,
		breaker.metrics.CountSuccess,
		breaker.metrics.CountFail,
		breaker.metrics.ConsecutiveSuccess,
		breaker.metrics.ConsecutiveFail,
		breaker.metrics.WindowTimeStart.Format("2006/1/2 15:04:05"))

	return result, err
}

// before intecept  The fuse calls beforeCall() before execution to determine whether it can be executed
func (breaker *ServiceBreaker) beforeCall() error {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	now := time.Now()
	switch breaker.state {
	case StateOpen:
		//after sleep timeout, can retry  如过冷却时间，进入半开状态尝试调用。
		if breaker.stateOpenTime.Add(breaker.sleepTimeout).Before(now) {
			log.Printf("%s 熔断过冷却期，尝试半开\n", breaker.name)
			breaker.changeState(StateHalfOpen, now)
			return nil
		}
		log.Printf("%s 熔断打开，请求被阻止\n", breaker.name)
		return ErrStateOpen
	case StateHalfOpen: //半开状态，放halfMaxCalls请求通过进行试探
		if breaker.metrics.CountAll >= breaker.halfMaxCalls {
			log.Printf("%s 熔断半开，请求过多被阻止\n", breaker.name)
			return ErrTooManyCalls
		}

	default: //Closed	关闭状态，不做拦截，
		if !breaker.metrics.WindowTimeStart.IsZero() && breaker.metrics.WindowTimeStart.Before(now) {
			breaker.nextWindow(now)
			return nil
		}

	}
	return nil
}

// after intercept    After execution, call afterCall() to perform indicator statistics and status updates
func (breaker *ServiceBreaker) afterCall(success bool) {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	if success {
		breaker.onSuccess(time.Now())
	} else {
		breaker.onFail(time.Now())
	}

}

// call success
func (breaker *ServiceBreaker) onSuccess(now time.Time) {
	breaker.metrics.OnSuccess()
	if breaker.state == StateHalfOpen && breaker.metrics.ConsecutiveSuccess >= breaker.halfMaxCalls {
		breaker.changeState(StateClosed, now)
	}
}

// call fail
// afterCall()  发现调用失败先做失败统计，
// 状态半开，如果失败了直接转为关闭，严格模式。
// 状态关闭，会根据策略判断是否要开启熔断。
func (breaker *ServiceBreaker) onFail(now time.Time) {
	breaker.metrics.OnFail()
	switch breaker.state {
	case StateClosed:
		if breaker.tripStrategyFunc(breaker.metrics) {
			breaker.changeState(StateOpen, now)
		}
	case StateHalfOpen:
		breaker.changeState(StateOpen, now)

	}
}

// change breaker state
func (breaker *ServiceBreaker) changeState(state State, now time.Time) {
	if breaker.state == state {
		return
	}
	prevState := breaker.state
	breaker.state = state
	//goto next window,reset metrics
	breaker.nextWindow(time.Now())
	//record open time
	if state == StateOpen {
		breaker.stateOpenTime = now
	}
	//callback hook
	if breaker.stateChangeHook != nil {
		breaker.stateChangeHook(breaker.name, prevState, state)
	}
}

// goto next time window  在初始化熔断器和熔断器状态变更的时候都会新开统计窗口  开启新的窗口批次，所有计数清零。
func (breaker *ServiceBreaker) nextWindow(now time.Time) {
	breaker.metrics.NewBatch()
	breaker.metrics.OnReset() //clear count num
	var zero time.Time
	switch breaker.state {
	case StateClosed:
		if breaker.windowInterval == 0 {
			breaker.metrics.WindowTimeStart = zero
		} else {
			breaker.metrics.WindowTimeStart = now.Add(breaker.windowInterval)
		}
	case StateOpen:
		breaker.metrics.WindowTimeStart = now.Add(breaker.sleepTimeout)
	default: //halfopen
		breaker.metrics.WindowTimeStart = zero //halfopen no window
	}
}

func (breaker *ServiceBreaker) OpenBreaker() {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	if breaker.state == StateOpen {
		return
	}
	log.Printf("手工打开熔断器: %s\n", breaker.name)
	breaker.changeState(StateOpen, time.Now())
}

func (breaker *ServiceBreaker) CloseBreaker() {
	breaker.mu.Lock()
	defer breaker.mu.Unlock()
	if breaker.state == StateClosed {
		return
	}
	log.Printf("手工关闭熔断器: %s\n", breaker.name)
	breaker.changeState(StateClosed, time.Now())
}

func (breaker *ServiceBreaker) State() State {
	breaker.mu.RLock()
	defer breaker.mu.RUnlock()
	return breaker.state
}
