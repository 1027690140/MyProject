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

	workers []*Worker
	//通知关闭
	release chan stop

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
		workers:         make([]*Worker, 0),
		release:         make(chan stop, 1),
	}

	go pool.monitor()

	return pool, nil
}

// NewWorker creates a new worker associated with a pool.
func (p *Pool) NewWorker(task taskFunc) *Worker {
	return &Worker{
		pool: p,
		task: make(chan taskFunc, 1),
	}
}

// Start starts the worker to receive task.
func (w *Worker) Start() {
	go func() {
		for {
			w.pool.AddWorker(w)
			task := <-w.task
			if task == nil {
				break
			}
			task()
			w.pool.Done()
		}
	}()
}

// Stop stops the worker from receiving new task.
func (w *Worker) Stop() {
	w.task <- nil
}

// IsExpired checks if this worker is expired.
func (w *Worker) IsExpired() bool {
	return w.lastUsed.Add(w.pool.expiredDuration).Before(time.Now())
}

// SetLastUsed sets the last used time to now.
func (w *Worker) SetLastUsed() {
	w.lastUsed = time.Now()
}

// AddWorker adds a worker back to the pool.
func (p *Pool) AddWorker(worker *Worker) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if worker.IsExpired() {
		worker.Stop()
		return
	}
	p.workers = append(p.workers, worker)
	worker.SetLastUsed()
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

	if len(p.release) > 0 {
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
	})
}

func (p *Pool) getWorker() *Worker {
	var w *Worker
	// 标志变量，判断当前正在运行的worker数量是否已到达Pool的容量上限
	waiting := false
	// 加锁，检测队列中是否有可用worker，并进行相应操作
	p.lock.RLock()
	idleWorkers := p.workers
	n := len(idleWorkers) - 1
	// 当前队列中无可用worker
	if n < 0 {
		// 判断运行worker数目已达到该Pool的容量上限，置等待标志
		waiting = p.active >= p.capacity

		// 当前队列有可用worker，从队列尾部取出一个使用
	} else {
		w = idleWorkers[n]
		idleWorkers[n] = nil
		p.workers = idleWorkers[:n]
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

func (p *Pool) incRunning() {
	atomic.AddInt32(&p.active, 1)
}
func (p *Pool) decRunning() {
	atomic.AddInt32(&p.active, -1)
}

func (p *Pool) putWorker(worker *Worker) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if worker.isExpired() {
		worker.stop()
		return
	}
	p.workers = append(p.workers, worker)
	worker.setLastUsed()
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
