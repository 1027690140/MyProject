package model

import (
	"service_discovery/configs"
	"sync"
	"sync/atomic"
)

/*
self-protection mechanism
Turn on the protection according to the proportion of failures in a short period of time reaching a certain threshold
*/
type Guard struct {
	renewCount     int64
	lastRenewCount int64
	needRenewCount int64
	threshold      int64
	lock           sync.RWMutex
}

func (gd *Guard) incrNeed() {
	gd.lock.Lock()
	defer gd.lock.Unlock()
	gd.needRenewCount += int64(configs.CheckEvictInterval / configs.RenewInterval)
	gd.threshold = int64(float64(gd.needRenewCount) * configs.SelfProtectThreshold)
}

func (gd *Guard) decrNeed() {
	gd.lock.Lock()
	defer gd.lock.Unlock()
	gd.needRenewCount -= int64(configs.CheckEvictInterval / configs.RenewInterval)
	gd.threshold = int64(float64(gd.needRenewCount) * configs.SelfProtectThreshold)
}

func (gd *Guard) setNeed(count int64) {
	gd.lock.Lock()
	defer gd.lock.Unlock()
	gd.needRenewCount = count * int64(configs.CheckEvictInterval/configs.RenewInterval)
	gd.threshold = int64(float64(gd.needRenewCount) * configs.SelfProtectThreshold)
}

func (gd *Guard) incrCount() {
	atomic.AddInt64(&gd.renewCount, 1)
}

func (gd *Guard) storeLastCount() {
	atomic.StoreInt64(&gd.lastRenewCount, atomic.SwapInt64(&gd.renewCount, 0))
}

// 自我保护状态
func (gd *Guard) selfProtectStatus() bool {
	// 保护阈值
	return atomic.LoadInt64(&gd.lastRenewCount) < atomic.LoadInt64(&gd.threshold)
}
