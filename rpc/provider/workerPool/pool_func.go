// 参考 借鉴
package workerpool

import (
	"errors"
	"math"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// 默认容量。
	DEFAULT_POOL_SIZE = math.MaxInt32

	// 清理 goroutine 的间隔时间。
	DEFAULT_CLEAN_INTERVAL_TIME = 1

	// 表示池已关闭。
	CLOSED = 1
)

var (
	// Error types for the Ants API.
	//---------------------------------------------------------------------------

	// ErrInvalidPoolSize will be returned when setting a negative number as pool capacity.
	ErrInvalidPoolSize = errors.New("invalid size for pool")

	// ErrInvalidPoolExpiry will be returned when setting a negative number as the periodic duration to purge goroutines.
	ErrInvalidPoolExpiry = errors.New("invalid expiry for pool")

	// ErrPoolClosed will be returned when submitting task to a closed pool.
	ErrPoolClosed = errors.New("this pool has been closed")

	// ErrPoolOverload will be returned when the pool is full and no workers available.
	ErrPoolOverload = errors.New("too many goroutines blocked on submit or Nonblocking is set")
	//---------------------------------------------------------------------------

	// workerChanCap determines whether the channel of a worker should be a buffered channel
	workerChanCap = func() int {
		// Use blocking workerChan if GOMAXPROCS=1.
		if runtime.GOMAXPROCS(0) == 1 {
			return 0
		}

		return 1
	}()

	defaultPool, _ = NewPool(DEFAULT_POOL_SIZE, (DEFAULT_CLEAN_INTERVAL_TIME * time.Second))
)

// 特定func的pool
type PoolWithFunc struct {
	// 池容量
	capacity int32

	//前正在运行的 goroutine 的数量
	running int32

	// 过期时间（秒）
	expiryDuration time.Duration

	workers []*WorkerWithFunc

	// release 用于通知池自行关闭
	release int32

	lock sync.Mutex

	// cond 用于等待获得空闲的worker
	cond *sync.Cond

	// poolFunc是处理任务的函数
	poolFunc func(interface{})

	// 一旦确保释放该池只会完成一次
	once sync.Once

	// workerCache在函数retrieveWorker中加速了可用worker的获取
	workerCache sync.Pool

	// PanicHandler 用于处理来自每个工作协程的Panic
	PanicHandler func(interface{})

	// pool.Submit 上阻塞的 Goroutine 的最大数量
	MaxBlockingTasks int32

	// goroutine 已经被阻塞在 pool.Submit 上
	// 受 pool.lock 保护
	blockingNum int32

	// 当 Nonblocking 为 true 时，Pool.Submit 永远不会被阻塞
	// 当 Pool.Submit 无法立即完成时，将返回 ErrPoolOverload
	// 当 Nonblocking 为 true 时，MaxBlockingTasks 不起作用
	Nonblocking bool
}

// 定期清理过期Worker
func (p *PoolWithFunc) periodicallyPurge() {
	heartbeat := time.NewTicker(p.expiryDuration)
	defer heartbeat.Stop()

	var expiredWorkers []*WorkerWithFunc
	for range heartbeat.C {
		if atomic.LoadInt32(&p.release) == CLOSED {
			break
		}
		currentTime := time.Now()
		p.lock.Lock()
		idleWorkers := p.workers
		n := len(idleWorkers)
		i := 0
		for i < n && currentTime.Sub(idleWorkers[i].recycleTime) > p.expiryDuration {
			i++
		}
		expiredWorkers = append(expiredWorkers[:0], idleWorkers[:i]...)
		if i > 0 {
			m := copy(idleWorkers, idleWorkers[i:])
			for i = m; i < n; i++ {
				idleWorkers[i] = nil
			}
			p.workers = idleWorkers[:m]
		}
		p.lock.Unlock()

		// 通知过时的Worker停止
		// 这个通知必须在 p.lock 之外，因为 w.task
		// 如果有很多Worker，可能会阻塞并且可能会消耗大量时间
		// 位于非本地 CPU 上
		for i, w := range expiredWorkers {
			w.args <- nil
			expiredWorkers[i] = nil
		}

		// 可能存在所有worker都被清理掉的情况（没有任何worker在运行）
		// 虽然一些调用者仍然陷入“p.cond.Wait()”，
		// 那么它应该唤醒所有这些调用者
		if p.Running() == 0 {
			p.cond.Broadcast()
		}
	}
}

// NewPoolWithFunc 生成具有特定func 的
func NewPoolWithFunc(size int, pf func(interface{})) (*PoolWithFunc, error) {
	return NewUltimatePoolWithFunc(size, DEFAULT_CLEAN_INTERVAL_TIME, pf, false)
}

// NewPoolWithFuncPreMalloc 池大小的内存预分配。
func NewPoolWithFuncPreMalloc(size int, pf func(interface{})) (*PoolWithFunc, error) {
	return NewUltimatePoolWithFunc(size, DEFAULT_CLEAN_INTERVAL_TIME, pf, true)
}

// NewUltimatePoolWithFunc
func NewUltimatePoolWithFunc(size, expiry int, pf func(interface{}), preAlloc bool) (*PoolWithFunc, error) {
	if size <= 0 {
		return nil, ErrInvalidPoolSize
	}
	if expiry <= 0 {
		return nil, ErrInvalidPoolExpiry
	}
	var p *PoolWithFunc
	if preAlloc {
		p = &PoolWithFunc{
			capacity:       int32(size),
			expiryDuration: time.Duration(expiry) * time.Second,
			poolFunc:       pf,
			workers:        make([]*WorkerWithFunc, 0, size),
		}
	} else {
		p = &PoolWithFunc{
			capacity:       int32(size),
			expiryDuration: time.Duration(expiry) * time.Second,
			poolFunc:       pf,
		}
	}
	p.cond = sync.NewCond(&p.lock)
	go p.periodicallyPurge()
	return p, nil
}

//---------------------------------------------------------------------------

// Invoke submits a task to pool.
func (p *PoolWithFunc) Invoke(args interface{}) error {
	if atomic.LoadInt32(&p.release) == CLOSED {
		return ErrPoolClosed
	}
	if w := p.retrieveWorker(); w == nil {
		return ErrPoolOverload
	} else {
		w.args <- args
	}
	return nil
}

// Running returns the number of the currently running goroutines.
func (p *PoolWithFunc) Running() int {
	return int(atomic.LoadInt32(&p.running))
}

// Free returns a available goroutines to work.
func (p *PoolWithFunc) Free() int {
	return int(atomic.LoadInt32(&p.capacity) - atomic.LoadInt32(&p.running))
}

// Cap returns the capacity of this pool.
func (p *PoolWithFunc) Cap() int {
	return int(atomic.LoadInt32(&p.capacity))
}

// Tune change the capacity of this pool.
func (p *PoolWithFunc) Tune(size int) {
	if p.Cap() == size {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	diff := p.Running() - size
	for i := 0; i < diff; i++ {
		p.retrieveWorker().args <- nil
	}
}

// Release Closed this pool.
func (p *PoolWithFunc) Release() error {
	p.once.Do(func() {
		atomic.StoreInt32(&p.release, 1)
		p.lock.Lock()
		idleWorkers := p.workers
		for i, w := range idleWorkers {
			w.args <- nil
			idleWorkers[i] = nil
		}
		p.workers = nil
		p.lock.Unlock()
	})
	return nil
}

//---------------------------------------------------------------------------

// incRunning increases the number of the currently running goroutines.
func (p *PoolWithFunc) incRunning() {
	atomic.AddInt32(&p.running, 1)
}

// decRunning decreases the number of the currently running goroutines.
func (p *PoolWithFunc) decRunning() {
	atomic.AddInt32(&p.running, -1)
}

// retrieveWorker 返回一个可用的工作线程来运行任务
func (p *PoolWithFunc) retrieveWorker() *WorkerWithFunc {
	var w *WorkerWithFunc
	spawnWorker := func() {
		if cacheWorker := p.workerCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*WorkerWithFunc)
		} else {
			w = &WorkerWithFunc{
				pool: p,
				args: make(chan interface{}, workerChanCap),
			}
		}
		w.run()
	}

	p.lock.Lock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	if n >= 0 {
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
		p.lock.Unlock()
	} else if p.Running() < p.Cap() {
		p.lock.Unlock()
		spawnWorker()
	} else {
		if p.Nonblocking {
			p.lock.Unlock()
			return nil
		}
	Reentry:
		if p.MaxBlockingTasks != 0 && p.blockingNum >= p.MaxBlockingTasks {
			p.lock.Unlock()
			return nil
		}
		p.blockingNum++
		p.cond.Wait()
		p.blockingNum--
		if p.Running() == 0 {
			p.lock.Unlock()
			spawnWorker()
			return w
		}
		l := len(p.workers) - 1
		if l < 0 {
			goto Reentry
		}
		w = p.workers[l]
		p.workers[l] = nil
		p.workers = p.workers[:l]
		p.lock.Unlock()
	}
	return w
}

// revertWorker worker放回空闲池，回收 goroutine
func (p *PoolWithFunc) revertWorker(worker *WorkerWithFunc) bool {
	if atomic.LoadInt32(&p.release) == CLOSED {
		return false
	}
	worker.recycleTime = time.Now()
	p.lock.Lock()
	p.workers = append(p.workers, worker)
	// Notify the invoker stuck in 'retrieveWorker()' of there is an available worker in the worker queue.
	p.cond.Signal()
	p.lock.Unlock()
	return true
}
