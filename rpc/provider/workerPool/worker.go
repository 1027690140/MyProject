package workerpool

import "time"

// Worker is a worker in the pool.
type Worker struct {
	pool *Pool

	task chan taskFunc

	lastUsed time.Time
}

func (w *Worker) stop() {
	// 创建一个带缓冲区的 channel，容量与需要清空的 channel 容量一样。
	newCh := make(chan taskFunc, cap(w.task))
	// 用新 channel 取代旧 channel。
	w.task = newCh
}
func (w *Worker) isExpired() bool {
	return w.lastUsed.Add(w.pool.expiredDuration).Before(time.Now())
}
func (w *Worker) setLastUsed() {
	w.lastUsed = time.Now()
}
func (w *Worker) run() {
	go func() {
		// 循环监听任务列表，一旦有任务立马取出运行
		for f := range w.task {
			if f == nil {
				// 退出goroutine，运行worker数减一
				w.pool.decRunning()
				close(w.task)
				return
			}
			f()
			// worker回收复用
			w.pool.putWorker(w)
		}
	}()
}
