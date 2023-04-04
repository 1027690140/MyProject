package pool

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

// TODO 优先队列分配
// TODO 多address conn
// TODO 活跃空闲 conn 管理

// poolChecker 存放连接检查信息
type poolChecker struct {
	indexFreq         time.Duration // 获取index超时时间
	connectionTimeout time.Duration // 连接超时时间
	idleTimeout       time.Duration // 空闲连接超时时间
	idleCheckFreq     time.Duration // 空闲连接检查频率
	keepAliveInterval time.Duration // 保活检查时间
	keepAliveStopChan chan struct{} //关闭keepalive
	keepAliveChan     chan struct{} //通知keepalive
}

// 锁
type poolLocker struct {
	lock          sync.RWMutex // 锁
	expandingLock sync.Mutex   // 扩容锁
	shrinkingLock sync.Mutex   // 缩容锁
	idlecheckLock sync.RWMutex // idlecheckLock锁
	keepaliveLock sync.RWMutex // keepalive检查锁
}

// ConnectionPool 某个addr的连接池
type ConnectionPool struct {
	poolLocker // 锁
	poolChecker
	addr        string
	newConnFunc func(string) (net.Conn, error)

	connections []chan *poolConn // 连接池 放poolConn
	lastused    []time.Time      // 连接池最后使用时间，用于connections[]缩容

	reuse bool // // 如果 reuse 为真且池处于 MaxActive 限制，则 Get() 重用 要返回的连接，如果 reuse 为 false 并且池处于 MaxActive 限制， 创建一个一次性连接返回。

	roundRobinCounter int32 // index

	minConns    int // 最小连接数
	maxConns    int // 最大连接数
	maxIdle     int // 最大空闲连接数
	currConns   int // 当前连接数 = 被取出的+还在池子里的
	poolNum     int // connections大小
	shrinkNum   int // 缩容数目
	connChanNum int //  []chan *poolConn 中每个chan大小

	timerPool *sync.Pool // 池化保存 Timer

	closed     bool          // 是否关闭
	poolClosed chan struct{} // 通知关闭
}

// NewConnectionPool 初始化连接池
func NewConnectionPool(newConnFunc func(addr string) (net.Conn, error), option *PoolOptions) (*ConnectionPool, error) {

	if option == nil {
		return nil, ErrPoolsOptionNotExist
	}
	if newConnFunc == nil {
		return nil, ErrNewConnFunc
	}
	if option.addr == "" {
		return nil, ErrAddress
	}
	if option.maxIdle <= 0 || option.maxConns <= 0 || option.minConns > option.maxConns {
		return nil, ErrMaximumParameter
	}
	if option.maxConns <= 0 {
		return nil, ErrMaxConnsParameter
	}
	var timerPool = &sync.Pool{
		New: func() interface{} {
			return time.NewTimer(option.indexFreq)
		},
	}

	p := &ConnectionPool{
		poolLocker: poolLocker{},
		poolChecker: poolChecker{
			connectionTimeout: option.connectionTimeout,
			indexFreq:         option.indexFreq,
			idleTimeout:       option.idleTimeout,
			idleCheckFreq:     option.idleCheckFreq,
			keepAliveInterval: option.keepAliveInterval,
			keepAliveStopChan: make(chan struct{}),
			keepAliveChan:     make(chan struct{}, option.poolNum),
		},
		addr:        option.addr,
		newConnFunc: newConnFunc,
		connections: make([]chan *poolConn, option.poolNum),
		lastused:    make([]time.Time, option.maxConns),
		minConns:    option.minConns,
		maxConns:    option.maxConns,
		currConns:   0,
		poolNum:     option.poolNum,
		connChanNum: option.maxConns / option.poolNum,
		poolClosed:  make(chan struct{}),
		closed:      false, timerPool: timerPool,
		reuse:             option.reuse,
		roundRobinCounter: 0,
	}

	// 初始化连接池中的连接
	for i := 0; i < option.minConns; i++ {
		p.createNewConn()
	}

	// 检查连接是否超时
	go p.KeepAliveCheck()

	return p, nil
}

// KeepAliveCheck 检查连接是否超时
func (p *ConnectionPool) KeepAliveCheck() {

	ticker := time.NewTicker(p.keepAliveInterval)
	defer ticker.Stop()

	for {

		select {
		// 检查连接池中所有连接的超时状态
		case <-p.keepAliveChan:
		case <-ticker.C:
			p.keepaliveLock.RLock()

			// 连接池已关闭
			if p.closed {
				p.keepaliveLock.RUnlock()
				continue
			}

			// keepAlive等待组
			keepAliveWaitGroup := sync.WaitGroup{}
			poollen := len(p.connections)
			keepAliveWaitGroup.Add(poollen)

			for i := 0; i < poollen; i++ {

				len := len(p.connections[i])

				go func(len, i int) {
					defer keepAliveWaitGroup.Done()
					for j := 0; j < len; j++ {
						conn := <-p.connections[i]
						if err := p.keepAlive(conn); err != nil {
							_ = conn.Close()
							p.currConns--
						} else {
							p.connections[i] <- conn
							// 更新lastused
							if time.After(conn.lastUsedTime.Sub(p.lastused[i])) != nil {
								p.lastused[i] = conn.lastUsedTime
							}
						}
					}
				}(len, i)
			}

			keepAliveWaitGroup.Wait()

			// 如果连接池中的连接数小于最小连接数，则扩容
			poollen = len(p.connections)

			if poollen < p.minConns {
				go p.expand()
			}

			p.keepaliveLock.RUnlock()

		case <-p.keepAliveStopChan:
			close(p.keepAliveChan)
			// 停止空闲连接检查
			return

		}
	}

}

func (p *ConnectionPool) createNewConn() error {
	p.expandingLock.Lock()
	defer p.expandingLock.Unlock()

	// 检查连接数是否已达到最大值
	if p.currConns >= p.maxConns {
		return ErrMaxConnsReached
	}

	// 创建新连接
	conn, err := p.newConnFunc(p.addr)
	if err != nil {
		fmt.Println("Failed to create new connection:", err)
		return err
	}

	// 初始化poolConn并添加到连接池中
	pc := &poolConn{
		conn:         conn,
		pool:         p,
		lastUsedTime: time.Now(),
	}
	p.currConns++

	// 通过通道将连接添加到连接池中
	connChan := make(chan *poolConn, p.connChanNum)
	connChan <- pc
	p.connections = append(p.connections, connChan)
	return nil
}

// 获取conn时  用roundRobin算法，对池数量取模，模的下标就是数组的索引，然后根据索引取对应的channel
func (p *ConnectionPool) indexGet() int {

	indexRes := make(chan int, p.poolNum*2)
	ctx, cancel := context.WithTimeout(context.Background(), p.indexFreq)

	defer close(indexRes)
	defer cancel()

	for i := 0; i < p.poolNum; i++ {
		go func(ctx context.Context) {
			index := int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			//单前index大于poolNum，换一个
			if index >= p.poolNum {
				index = int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			}
			//单前index下的chan空了，换一个
			if len(p.connections[index]) == 0 {
				index = int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			}

			if len(p.connections[index]) > 0 && index < p.poolNum {
				select {
				case indexRes <- index:
				case <-ctx.Done():
					return
				default:
					// 通道满，只需返回一个index即可
					return
				}
			}
		}(ctx)
	}
	select {
	case res := <-indexRes:
		return res
	case <-ctx.Done():
		return 0
	}
}

// 放回conn时 index
func (p *ConnectionPool) indexPut() int {

	indexRes := make(chan int, p.poolNum*2)
	ctx, cancel := context.WithTimeout(context.Background(), p.indexFreq)

	//first cancel  then close
	defer close(indexRes)
	defer cancel()

	for i := 0; i < p.poolNum; i++ {

		go func(ctx context.Context) {
			index := int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			//单前index下的chan满了，换一个
			if len(p.connections[index]) >= p.connChanNum {
				index = int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			}
			//单前index下的chan满了，换一个
			if len(p.connections[index]) >= p.connChanNum {
				index = int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.poolNum))
			}
			if len(p.connections[index]) < p.connChanNum && index < p.poolNum {
				select {
				case indexRes <- index:
				case <-ctx.Done():
					// 超时或被取消，退出协程
					return
				}
			}
		}(ctx)
	}
	select {
	case res := <-indexRes:
		return res
	case <-ctx.Done():
		// 超时
		return 0
	}

}

// //  递归爆栈  尝试信号量
// func (p *ConnectionPool) index() int {
// 	p.sem.Acquire(context.Background(), 1)
// 	defer p.sem.Release(1)

// 	timer := p.timerPool.Get().(time.Timer)
//	timer.Reset(time.Duration(0))
// 	defer p.timerPool.Put(timer)

// 	indexRes := make(chan int, 1)

// 	go func() {
// 		index := int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.currConns))
// 		//单前index下的chan满了，换一个
// 		if len(p.connections[index]) >= p.connChanNum {
// 			index = p.index()
// 		}
// 		//单前index下的chan空了，换一个
// 		if len(p.connections[index]) == 0 {
// 			index = p.index()
// 		}
// 		indexRes <- index
// 	}()

// 	select {
// 	// 找不到，此时很可能被锁了
// 	case <-timer.C:
// 		return int(atomic.AddInt32(&p.roundRobinCounter, 1) % int32(p.currConns))
// 	case index := <-indexRes:
// 		return index
// 	}
// }

// Get 获取连接
func (p *ConnectionPool) Get() (net.Conn, error) {

	// 加锁，保证线程安全
	p.lock.Lock()
	defer p.lock.Unlock()

	if p.closed {
		return nil, ErrClosedConnectionPool
	}

	//连接池为空，则创建新连接
	if p.currConns == 0 {
		defer func() {
			go p.expand()
		}()

		// 如果 不是是连接重用
		if !p.reuse {
			// 返回一次性连接
			c, err := p.newConnFunc(p.addr)
			go p.expand()
			return c, err

		}

		return nil, ErrPoolEmpty
	}

	// 如果当前连接数小于阈值数，则扩容
	if p.currConns < p.shrinkNum {
		defer func() {
			go p.expand()
		}()
	}

	// 如果没有空闲连接且已经达到最大连接数 连接池已满 ，则等待有连接可用或者超时
	timer := time.NewTimer(p.connectionTimeout)
	defer timer.Stop()
	index := p.indexGet()
	select {
	case conn := <-p.connections[index]:
		// 检查连接是否可用
		if err := p.checkConn(conn); err != nil {
			conn.Close()
			p.currConns--
			c, _ := p.newConnFunc(p.addr)
			//close 了一个，总数不变
			return c, err
		}
		return conn.conn, nil
	case <-timer.C:
		// 获取超时,容量不够
		go p.expand()
		return nil, ErrGetConnectionTimeout
	}

}

// checkConn函数用于检查连接是否过期。如果连接超时，则从连接池中删除该连接
func (p *ConnectionPool) checkConn(pc *poolConn) error {
	if time.Since(pc.lastUsedTime) > p.idleTimeout {
		p.lock.Lock()
		p.currConns--
		p.lock.Unlock()
		pc.conn.Close()
		return ErrTimeout
	}
	time.AfterFunc(p.keepAliveInterval, func() {
		p.checkConn(pc)
	})
	return nil

}

// Put 将连接放回连接池
func (p *ConnectionPool) Put(conn net.Conn) error {
	p.lock.Lock()
	defer p.lock.Unlock()
	if p.closed {
		conn.Close()
		// ErrConnectionPoolClosed
		return nil
	}

	//清空 连接的缓冲区 而不关闭连接
	err := conn.SetDeadline(time.Now())
	if err != nil {
		if p.reuse {
			conn.Close()
			return nil
		}
		conn, _ = p.newConnFunc(p.addr)
	}
	poolConn := &poolConn{conn: conn, pool: p, lastUsedTime: time.Now()}

	index := p.indexPut()
	timer := p.timerPool.Get().(time.Timer)
	//可能会在尚未触发的情况下被重复使用。调用 Reset 函数
	timer.Reset(time.Duration(0))
	defer p.timerPool.Put(timer)

	select {
	// 连接成功放回连接池
	case p.connections[index] <- poolConn:
		//更新时间
		p.lastused[index] = poolConn.lastUsedTime

	case <-timer.C:
		// // 连接池已满，关闭连接
		// conn.Close()

		// 当前连接数减1
		p.currConns--

		// 开始缩容
		go p.shrink()

		return ErrPoolFull
	}

	return nil
}

// wrapConn 包装连接以实现 keep-alive
func (p *ConnectionPool) wrapConn(conn net.Conn) net.Conn {
	return &poolConn{conn: conn, pool: p, lastUsedTime: time.Now()}
}

// expand 扩容连接池
func (p *ConnectionPool) expand() error {

	p.expandingLock.Lock()
	defer p.expandingLock.Unlock()
	// 如果当前连接数已经达到最大连接数，则无法再扩容
	if p.currConns == p.maxConns {
		return errors.New("connection pool has reached max connections")
	}

	// 如果有其他扩容任务已经在进行中，则等待
	if len(p.connections)+p.currConns < p.maxConns {
		return nil
	}
	// 计算可以扩容的最大数量
	maxExpand := p.maxConns - p.currConns
	if maxExpand <= 0 {
		return nil
	}

	maxExpand = maxExpand / p.connChanNum
	// 扩容数量不能超过可用的 chan 数组的长度
	if maxExpand+len(p.connections) <= p.poolNum {
		maxExpand = p.poolNum - len(p.connections)
	}
	for i := 0; i < maxExpand; i++ {
		connChan := make(chan *poolConn, p.connChanNum)
		p.connections = append(p.connections, connChan)

		conn, err := p.newConnFunc(p.addr)
		if err != nil {
			// 扩容失败，需要回收已经申请到的连接
			for j := 0; j < i; j++ {
				conn := <-p.connections[j]
				_ = conn.Close()
				p.currConns--
			}
			return err
		}
		p.connections[len(p.connections)-1] <- conn.(*poolConn)
		p.currConns++
	}

	return nil
}

// shrink 缩容连接池
func (p *ConnectionPool) shrink() error {
	p.shrinkingLock.Lock()
	defer p.shrinkingLock.Unlock()
	// 如果连接池已经为空，则无法再缩容
	if len(p.connections) == 0 {
		return errors.New("connection pool is already empty")
	}

	// 如果有其他缩容任务已经在进行中，则等待
	if len(p.connections)+p.currConns > p.maxConns {
		return nil
	}
	if p.shrinkNum != 0 {

		if p.currConns > p.shrinkNum {

			// 计算需要关闭的连接数量
			numConnsToClose := p.currConns - p.shrinkNum

			// 对连接池中的所有连接按最后使用时间从新到旧排序()
			sort.Slice(p.connections, func(i, j int) bool {
				return p.lastused[j].Before(p.lastused[i])
			})

			// 关闭连接池中最旧的 numConnsToClose 个连接
			for i := 0; i < numConnsToClose; i++ {

				// 关闭
				closeConn := p.connections[len(p.connections)-1]

				for c := range closeConn {
					conn := c
					conn.Close()
					p.currConns--
				}

				close(closeConn)
				// 将连接池中的连接从切片中移除
				p.connections = p.connections[len(p.connections):]
			}
		}
	}

	return nil
}

// ClosePool 关闭连接池
func (p *ConnectionPool) ClosePool() error {
	p.lock.Lock()
	defer p.lock.Unlock()
	// 关闭连接池等待组
	stopWaitGroup := sync.WaitGroup{}

	stopWaitGroup.Add(len(p.connections))

	// 关闭所有连接通道
	for i := 0; i < len(p.connections); i++ {
		// 回收连接池中所有连接
		connCh := p.connections[i]
		go func(connCh chan *poolConn) {
			defer stopWaitGroup.Done()
			for len(connCh) > 0 {
				conn := <-connCh
				_ = conn.Close()
				p.currConns--
			}
			close(connCh)
		}(connCh)
	}
	stopWaitGroup.Wait()

	// 关闭保活协程
	close(p.keepAliveStopChan)
	close(p.poolClosed)
	p.closed = true

	return nil

}

// Finalizer  GC
func (p *ConnectionPool) Finalizer() {
	log.Printf("ConnectionPool %s is closed", p.addr)
}

// keepAlive  检查连接是否超时，如果超时则尝试重新建立连接
func (p *ConnectionPool) keepAlive(conn net.Conn) error {
	// 如果连接已经超时，则关闭连接并尝试重新建立连接
	if time.Now().Sub(conn.(*poolConn).lastUsedTime) > p.idleTimeout {
		_ = conn.Close()
		p.currConns--
		//有空余容量
		if p.currConns < p.maxConns {
			newConn, err := p.newConnFunc(p.addr)
			if err != nil {
				return err
			}
			pc := &poolConn{conn: newConn, pool: p, lastUsedTime: time.Now()}
			index := p.indexPut()
			p.connections[index] <- pc
			// 更新时间
			p.lastused[index] = time.Now()
			p.currConns++
		}
	} else {

		// 如果连接未超时，则更新连接

		// 重新设置超时时间
		if err := conn.SetDeadline(time.Now().Add(p.idleTimeout)); err != nil {
			p.releaseConn(conn)
		}
		conn.(*poolConn).lastUsedTime = time.Now()
	}
	return nil
}

func (p *ConnectionPool) releaseConn(conn net.Conn) error {
	if conn == nil {
		return errors.New("connection is nil")
	}
	// 如果连接池已经关闭，则关闭连接
	if p.closed {
		return conn.Close()
	}
	p.lock.Lock()
	defer p.lock.Unlock()

	index := p.indexPut()

	timer := p.timerPool.Get().(time.Timer)
	//可能会在尚未触发的情况下被重复使用。调用 Reset 函数
	timer.Reset(time.Duration(0))
	defer p.timerPool.Put(timer)

	// 检查连接池是否已满
	select {
	// 如果连接池未满，则将连接放回连接池
	case p.connections[index] <- &poolConn{conn: conn, pool: p, lastUsedTime: time.Now()}:
		p.lastused[index] = time.Now()
		return nil

	// 如果连接池已满，则关闭连接
	case <-timer.C:
		if index == -1 {
			// 连接数减一
			p.currConns--
			return conn.Close()
		}
	}
	return nil
}

// 返回最新使用时间
func (p *ConnectionPool) lastTime() time.Time {
	// 对连接池中的所有连接按最后使用时间从新到旧排序()
	sort.Slice(p.connections, func(i, j int) bool {
		return p.lastused[j].Before(p.lastused[i])
	})
	return p.lastused[0]
}
