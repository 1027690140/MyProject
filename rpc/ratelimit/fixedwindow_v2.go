package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

type CountLimiter struct {
	rate  int64         // 计数周期内运行的最大请求数
	begin time.Time     // 当前轮计数开始时间
	count int64         // 当前计数周期累计的请求数
	cycle time.Duration // 计数周期，如统计1秒内的总请求数，那么计数周期就是1秒
	lock  sync.Mutex    // 判断能否放行时需要加锁操作
}

func NewCountLimiter(rate int64, cycle time.Duration) *CountLimiter {
	return &CountLimiter{
		rate:  rate,
		begin: time.Now(),
		count: 0,
		cycle: cycle,
		lock:  sync.Mutex{},
	}
}

func (c *CountLimiter) Allow() bool {
	c.lock.Lock()
	defer c.lock.Unlock()
	if c.count == c.rate { // 达到最大限流时，判断时间是否也超过统计周期了，超了可以重置限流器了，没有超说明还在当前统计周期内，应该拦截
		if time.Now().Sub(c.begin) > c.cycle {
			c.Reset()
			return true
		} else {
			return false
		}
	} else { // 还没有达到最大限流数，那么不管时间周期是否超出统计计数周期，都可以放行
		c.count++
		return true
	}
}

func (c *CountLimiter) Reset() {
	c.begin = time.Now()
	c.count = 0
}

func testFix() {
	countLimiter := NewCountLimiter(3, time.Second) // 1s内不可超过3个请求
	var wg sync.WaitGroup
	wg.Add(10)
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer wg.Done()
			if countLimiter.Allow() {
				fmt.Println(fmt.Sprintf("当前时间%s,请求%d通过了", time.Now().String(), i))
			} else {
				fmt.Println(fmt.Sprintf("当前时间%s,请求%d被限流了", time.Now().String(), i))
			}
		}(i)
		time.Sleep(200 * time.Millisecond)
	}
	wg.Wait()
}
