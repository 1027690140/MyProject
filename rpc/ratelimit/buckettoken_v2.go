package ratelimit

import (
	"fmt"
	"sync"
	"time"
)

type TokenBucketLimiter struct {
	lock     sync.Mutex
	rate     time.Duration // 多长时间放入一个令牌，即放入令牌的速率
	capacity int64         // 令牌桶的容量，控制最多放入多少令牌，也即突发最大并发量
	tokens   int64         // 当前桶中已有的令牌数量
	lastTime time.Time     // 上次放入令牌的时间，避免开启协程定时去放入令牌，而是请求到来时懒加载的方式(now - lastTime) / rate放入令牌
}

func NewTokenBucketLimiter(rate time.Duration, capacity int64) *TokenBucketLimiter {
	if capacity < 1 {
		panic("token bucket capacity must be large 1")
	}
	return &TokenBucketLimiter{
		lock:     sync.Mutex{},
		rate:     rate,
		capacity: capacity,
		tokens:   0,
		lastTime: time.Time{},
	}
}

func (tbl *TokenBucketLimiter) Allow() bool {
	tbl.lock.Lock() // 加锁避免并发错误
	defer tbl.lock.Unlock()

	// 如果 now 与上次请求的间隔超过了 token rate
	// 则增加令牌，更新lastTime
	now := time.Now()
	if now.Sub(tbl.lastTime) > tbl.rate {
		tbl.tokens += int64((now.Sub(tbl.lastTime)) / tbl.rate) // 放入令牌
		if tbl.tokens > tbl.capacity {
			tbl.tokens = tbl.capacity // 总令牌数不能大于桶的容量
		}
		tbl.lastTime = now // 更新上次往桶中放入令牌的时间
	}

	if tbl.tokens > 0 { // 令牌数是否充足
		tbl.tokens -= 1
		return true
	}

	return false // 令牌不足，拒绝请求
}

func testbuckettoken2() {
	tbl := NewTokenBucketLimiter(10, 5) // 每10ms放一个令牌，1s放100个，桶容量（最大突发流量）为5
	for i := 0; i < 10; i++ {
		fmt.Println(tbl.Allow()) // 模拟突发流量10个请求，超过桶容量5
	}
	time.Sleep(100 * time.Millisecond)
	fmt.Println(tbl.Allow())
}
