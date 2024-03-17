package workerpool

import (
	"log"
	"runtime"
	"time"
)

// WorkerWithFunc 是运行任务的实际执行者，
// 它启动一个接受任务的 goroutine 执行函数调用。
type WorkerWithFunc struct {
	// pool who owns this worker.
	pool *PoolWithFunc

	// args is a job should be done.
	args chan interface{}

	// recycleTime will be update when putting a worker back into queue.
	recycleTime time.Time
}

// run starts a goroutine to repeat the process
// that performs the function calls.
func (w *WorkerWithFunc) run() {
	w.pool.incRunning()
	go func() {
		defer func() {
			if p := recover(); p != nil {
				w.pool.decRunning()
				w.pool.workerCache.Put(w)
				if w.pool.PanicHandler != nil {
					w.pool.PanicHandler(p)
				} else {
					log.Printf("worker with func exits from a panic: %v\n", p)
					var buf [4096]byte
					n := runtime.Stack(buf[:], false)
					log.Printf("worker with func exits from panic: %s\n", string(buf[:n]))
				}
			}
		}()

		for args := range w.args {
			if args == nil {
				w.pool.decRunning()
				w.pool.workerCache.Put(w)
				return
			}
			w.pool.poolFunc(args)
			if ok := w.pool.revertWorker(w); !ok {
				break
			}
		}
	}()
}
