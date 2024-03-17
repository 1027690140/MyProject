package workerpool

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type stop struct{}

type taskFunc func() error

// Pool 1.维护一个worker队列 2.提供Submit方法提交任务 3.提供Close方法关闭Pool
type Pool struct {
	// Pool的容量，即最大worker数量
	capacity int32
	// Pool中当前活跃的worker数量
	active int32
	// 过期时间，即worker最大空闲时间，超过该时间则worker会被关闭
	expiredDuration time.Duration

	// 无参函数 workers
	workers []*Worker

	// 带参函数 workers
	workersWithFunc []*WorkerWithFunc

	// poolFunc是处理任务的函数
	poolFunc func(interface{})

	//通知关闭
	release chan stop

	clsoed int32

	workerCache sync.Pool

	lock sync.RWMutex
	// 保证只关闭一次
	once sync.Once
}

// NewPool 1.创建一个新的Pool 2.启动一个协程，定时检查过期的worker并关闭
func NewPool(size int32, expiredDuration time.Duration) (*Pool, error) {
	if size <= 0 {
		return nil, fmt.Errorf("Pool size must be greater than 0")
	}

	pool := &Pool{
		capacity:        size,
		expiredDuration: expiredDuration,
		workers:         make([]*Worker, 0, size),
		workersWithFunc: make([]*WorkerWithFunc, 0, size),
		release:         make(chan stop, 1),
		workerCache: sync.Pool{
			New: func() interface{} {
				return &Worker{
					task: make(chan taskFunc, 1),
				}
			},
		},
	}

	go pool.monitor()
	go pool.periodicallyPurge()
	return pool, nil
}

// NewWorker creates a new worker associated with a pool.
func (p *Pool) NewWorker(task taskFunc) *Worker {
	return &Worker{
		pool: p,
		task: make(chan taskFunc, 1),
	}
}

// Done decreases the active worker count.
func (p *Pool) Done() {
	atomic.AddInt32(&p.active, -1)
}

// Submit submits a task to the pool.
func (p *Pool) Submit(task taskFunc) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	if len(p.release) > 0 || atomic.LoadInt32(&p.clsoed) != CLOSED {
		return fmt.Errorf("pool is closed")
	}
	if atomic.LoadInt32(&p.active) >= p.capacity {
		return fmt.Errorf("Pool is full")
	}

	// 如果当前活跃的worker数量小于Pool的容量，则创建一个新的worker
	if p.active < p.capacity {
		p.active++

		worker := &Worker{
			pool:     p,
			task:     make(chan taskFunc, 1),
			lastUsed: time.Now(),
		}

		p.workers = append(p.workers, worker)

		worker.task <- task

		return nil
	} else {
		worker := p.getWorker()
		worker.task <- task
	}

	// 如果当前没有空闲的worker且活跃的worker数量已达到Pool的容量，则返回错误
	return fmt.Errorf("no available worker")
}

// Close  pool
func (p *Pool) Close() {
	p.once.Do(func() {
		p.lock.RLock()
		defer p.lock.RUnlock()
		for i := 0; i < len(p.workers); i++ {
			p.workers[i].stop()
		}
		p.release <- stop{}
		close(p.release)
		p.clsoed = 1
	})
}

func (p *Pool) getWorker() *Worker {
	var w *Worker
	// 标志变量，判断当前正在运行的worker数量是否已到达Pool的容量上限
	waiting := false
	// 加锁，检测队列中是否有可用worker，并进行相应操作

	spawnWorker := func() {
		if cacheWorker := p.workerCache.Get(); cacheWorker != nil {
			w = cacheWorker.(*Worker)
		} else {
			w = &Worker{
				pool: p,
				task: make(chan taskFunc, 1),
			}
		}
		w.run()
	}

	p.lock.RLock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	// 当前队列有可用worker，从队列尾部取出一个使用
	if n >= 0 {
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]

		// 当前队列中无可用worker
	} else if p.active < p.capacity {
		spawnWorker()
	} else if n < 0 {
		// 判断运行worker数目已达到该Pool的容量上限，置等待标志
		waiting = p.active >= p.capacity

	}
	// 检测完成，解锁
	p.lock.RUnlock()
	// Pool容量已满，新请求等待
	if waiting {
		// 利用锁阻塞等待直到有空闲worker
		for {
			p.lock.RLock()
			idleWorkers = p.workers
			l := len(idleWorkers) - 1
			if l < 0 {
				p.lock.RUnlock()
				continue
			}
			w = idleWorkers[l]
			idleWorkers[l] = nil
			p.workers = idleWorkers[:l]
			p.lock.RUnlock()
			break
		}
		// 当前无空闲worker但是Pool还没有满，
		// 则可以直接新开一个worker执行任务
	} else if w == nil {
		w = &Worker{
			pool: p,
			task: make(chan taskFunc, 1),
		}
		w.run()
		// 运行worker数加一
		p.incRunning()
	}
	return w
}

// 监控 Pool 的状态 , 定时检查过期的 worker 并关闭
func (p *Pool) monitor() {
	ticker := time.NewTicker(p.expiredDuration)

	for {
		select {
		case <-p.release:
			for i := range p.workers {
				p.workers[i].stop()
			}
			return
		case <-ticker.C:
			p.lock.RLock()
			var expiredWorkers []*Worker
			for _, worker := range p.workers {
				if worker.isExpired() && p.active > p.capacity {
					expiredWorkers = append(expiredWorkers, worker)
					p.active--
				}
			}
			p.lock.RUnlock()

			for _, worker := range expiredWorkers {
				worker.stop()
			}
		}
	}
}

// 定期清理过期 Worker
// 定期检查空闲 worker 队列中是否有已过期的 worker 并清理：因为采用了 LIFO 后进先出队列存放空闲 worker，
// 所以该队列默认已经是按照 worker 的最后运行时间由远及近排序，可以方便地按顺序取出空闲队列中的每个 worker
// 并判断它们的最后运行时间与当前时间之差是否超过设置的过期时长，若是，则清理掉该 goroutine，释放该 worker，
// 并且将剩下的未过期 worker 重新分配到当前 Pool 的空闲 worker 队列中，进一步节省系统资源。
func (p *Pool) periodicallyPurge() {
	heartbeat := time.NewTicker(p.expiredDuration)
	for range heartbeat.C {
		currentTime := time.Now()
		p.lock.Lock()
		idleWorkers := p.workers
		if len(idleWorkers) == 0 && atomic.LoadInt32(&p.active) == 0 && len(p.release) > 0 {
			p.lock.Unlock()
			return
		}
		n := 0
		for i, w := range idleWorkers {
			if currentTime.Sub(w.recycleTime) <= p.expiredDuration {
				break
			}
			n = i
			w.task <- nil
			idleWorkers[i] = nil
		}
		n++
		if n >= len(idleWorkers) {
			p.workers = idleWorkers[:0]
		} else {
			p.workers = idleWorkers[n:]
		}
		p.lock.Unlock()
	}
}

func (p *Pool) incRunning() {
	atomic.AddInt32(&p.active, 1)
}
func (p *Pool) decRunning() {
	atomic.AddInt32(&p.active, -1)
}

func (p *Pool) putWorker(worker *Worker) bool {
	if atomic.LoadInt32(&p.clsoed) == CLOSED {
		return false
	}
	p.lock.RLock()
	defer p.lock.RUnlock()

	p.workers = append(p.workers, worker)
	worker.setLastUsed()
	return true
}

// ReSize capacity
func (p *Pool) ReSize(size int) {
	if size == int(p.capacity) {
		return
	}
	atomic.StoreInt32(&p.capacity, int32(size))
	diff := int(p.active) - size
	if diff > 0 {
		for i := 0; i < diff; i++ {
			p.getWorker().task <- nil
		}
	}
}
