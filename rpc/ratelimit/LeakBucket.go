package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"
)

type Result struct {
	Msg string // 根据实际情况定义返回结果需要哪些字段
}

type Handler func() Result // 处理函数的形式也应该根据具体需要而定

type Task struct {
	id      int64       // 任务id
	result  chan Result // 任务的执行结果，即请求的响应结果
	handler Handler     // 请求的执行函数
}

func NewTask(id int, handler Handler) Task {
	return Task{
		handler: handler,
		result:  make(chan Result),
		id:      int64(id),
	}
}

type LeakyBucketLimiter struct {
	bucketSize int64     // 桶的大小
	workerNum  int64     // 工作者数量，即最大并发数
	taskChan   chan Task // 用于存放请求
}

func NewLeakyBucketLimiter(bucketSize, workerNum int64) *LeakyBucketLimiter {
	if bucketSize < 1 {
		panic("bucketSize  must be large 1")
	}
	if workerNum < 1 {
		panic("workerNum must be large 1")
	}
	return &LeakyBucketLimiter{
		bucketSize: bucketSize,
		workerNum:  workerNum,
		taskChan:   make(chan Task, bucketSize),
	}
}

func (lbl *LeakyBucketLimiter) AddTask(task Task) bool { // 类似其他限流算法的Allow方法
	// 如果木桶已经满了，或者任务执行失败或超时了，返回false
	select {
	case lbl.taskChan <- task: // 利用了select的特性判断是否能往通道中添加任务
	default:
		fmt.Printf("请求%d被拒绝了\n", task.id)
		return false
	}

	// 如果成功入桶，调用者会等待Task的Handler执行结果
	// 由于Task的result是无缓冲的通道，不应该让其无限等待阻塞，否则出现问题时，不往该chan写，就会一直阻塞在这里了，泄漏
	// 因此设置一个超时时间
	//resp := <-task.result
	//fmt.Printf("请求%d运行成功，结果为：%v\n", task.id, resp)
	select {
	case resp := <-task.result:
		fmt.Printf("请求%d运行成功，结果为：%v\n", task.id, resp)
	case <-time.After(5 * time.Second): // 超时时间可以稍微设置长一点点，因为任务放入桶中后，可能需要排队一点时间才被拉取出来执行
		return false // 这里超时当被限流处理
	}

	return true
}

func (lbl *LeakyBucketLimiter) Start(ctx context.Context) {
	// 开启workerNum个协程从木桶拉取任务执行
	for i := 0; int64(i) < lbl.workerNum; i++ {
		go func(ctx context.Context) {
			defer func() { // 铁则：开启的子协程一定要捕获异常，否则一旦出现异常会依次上抛，上抛也一直不捕获，会导致程序退出
				if err := recover(); err != any(nil) {
					fmt.Println("捕获到异常")
				}
			}()

			for { // 持续监听，拉取任务执行
				select {
				case <-ctx.Done():
					fmt.Println("退出工作")
					return
				default:
					task := <-lbl.taskChan
					result := task.handler()
					task.result <- result // 处理结果写入对应Task的结果通道
				}
			}
		}(ctx)
	}
}

// 类似生产者消费者模型，添加任务即往队列中放任务（生产消息），
// 然后安排一定数量的协程（消费者）拉取任务执行（消费消息）。
func testLeakBucket() {
	bucket := NewLeakyBucketLimiter(10, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bucket.Start(ctx) // 开启消费者
	// 模拟20个并发请求
	var wg sync.WaitGroup
	wg.Add(20)
	for i := 0; i < 20; i++ {
		go func(id int) {
			defer wg.Done()
			task := NewTask(id, func() Result { // 这里的func应该根据实际需要定义为handler，写具体的业务逻辑和返回的result
				time.Sleep(300 * time.Millisecond) // 模拟业务逻辑消耗的时间
				return Result{}
			})
			bucket.AddTask(task) // 请求入桶
		}(i)
	}
	wg.Wait()
	time.Sleep(10 * time.Second)
}
