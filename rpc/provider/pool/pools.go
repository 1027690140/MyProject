package pool

import (
	"net"
	"runtime"
	"sync"
	"time"
)

// chan to stop
type poolStoper struct {
	checkIdleStopChan chan struct{} //关闭checkIdle
	retryNewStopChan  chan struct{} //关闭retry
	retryDelStopChan  chan struct{} //关闭retry
	retryPutStopChan  chan struct{} //关闭retry
	poolClosedChan    chan struct{} //通知关闭
}

// 全局pool 管理不同adrr 的pool  对所有的空闲、活跃连接进行管理
type Pools struct {
	poolStoper // chan to stop

	// 下面的连接数 指的是adrr种类数 类似set
	curActiveConnsNum int //当前活跃连接数
	curIdleConnsNum   int //当前空闲连接数  ： 即使用频率低于activeThreshold的连接
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	connPools map[string]*ConnectionPool //不同adrr对应的连接池

	activeThreshold int // 活跃阈值 超过该值就是活跃的连接

	connNum        map[string]int // 连接  统计所以连接以及对应的连接数
	activeConnsNum map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	idleConnsNum   map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	lastUsedTime   map[string]time.Time

	idleCheckFreq time.Duration // 空闲连接检查频率

	idlecheckLock sync.RWMutex // 空闲连接检查锁
	closeLock     sync.RWMutex // 关闭锁

	retryNewList chan *PoolOptions    // 重试新建队列
	retryDelList chan *ConnectionPool // 重试删除队列
	retryPutList chan []interface{}   // 重试put队列

	closed bool // 是否关闭
}

// NewPools return pools
func NewPools(opts []*PoolOptions, option *PoolsParameter) (*Pools, error) {
	if len(opts) == 0 {
		return nil, ErrPoolsParameterNotExist
	}

	pool := &Pools{
		poolStoper: poolStoper{
			retryNewStopChan:  make(chan struct{}),
			retryDelStopChan:  make(chan struct{}),
			retryPutStopChan:  make(chan struct{}),
			checkIdleStopChan: make(chan struct{}),
			poolClosedChan:    make(chan struct{}),
		},
		connPools:         make(map[string]*ConnectionPool),
		activeConnsNum:    make(map[string]int),
		idleConnsNum:      make(map[string]int),
		connNum:           make(map[string]int),
		retryNewList:      make(chan *PoolOptions, 100),
		retryDelList:      make(chan *ConnectionPool, 100),
		retryPutList:      make(chan []interface{}, 100),
		idleCheckFreq:     option.idleCheckFreq,
		activeThreshold:   option.activeThreshold,
		maxActiveConnsNum: option.maxActiveConnsNum,
		maxIdleConnsNum:   option.maxIdleConnsNum,
		curActiveConnsNum: 0,
		curIdleConnsNum:   0,
	}
	lens := len(opts)
	for i := 0; i < lens; i++ {
		go func(i int) {
			op := opts[i]
			cp, err := NewConnectionPool(op.newConnFunc, op)
			if err != nil {
				// 加入异步重试队列
				pool.retryNewList <- op
				return
			}
			pool.connPools[op.adrr] = cp
		}(i)
	}

	// // 检查连接是否超时
	// go pool.KeepAliveCheck()

	// 检查空闲连接
	go pool.idlecheck()

	// 开启重试
	go pool.retryNew()
	go pool.retryDelete()
	go pool.retryPut()

	return pool, nil
}

// NewPoolConnection 新建单个连接池
func (p *Pools) NewPoolConnection(op *PoolOptions) error {
	if op.adrr == "" {
		return ErrAddress
	}
	_, ok := p.connPools[op.adrr]

	if ok {
		return nil
	}
	pc, err := NewConnectionPool(op.newConnFunc, op)
	if err != nil {
		return err
	}
	p.connPools[op.adrr] = pc
	return nil
}

// GetConn 通过给定的adrr 获取连接
func (p *Pools) GetConn(adrr string) (net.Conn, error) {
	if p.closed {
		return nil, ErrPoolClosed
	}
	if _, ok := p.connPools[adrr]; !ok {
		return nil, ErrPoolNotExist
	}

	// // 如果有空闲连接，则直接返回
	// if conn := p.popActiveConn(adrr); conn != nil {
	// 	p.lastUsedTime[adrr] = time.Now()
	// 	return conn, nil
	// }

	conn, err := p.connPools[adrr].Get()
	if err == nil {
		// 统计连接
		go func() {
			// 更新时间
			p.lastUsedTime[adrr] = time.Now()

			if err == nil {
				// 统计连接数量
				p.connNum[adrr] = p.connPools[adrr].currConns

				// 由空闲连接 -> 活跃连接
				if p.connNum[adrr] >= p.activeThreshold {
					p.activeConnsNum[adrr] = p.connNum[adrr]
					//如果原先是空闲连接
					if _, ok := p.idleConnsNum[adrr]; ok {
						delete(p.activeConnsNum, adrr)
						delete(p.idleConnsNum, adrr)
						p.curIdleConnsNum--
						p.curActiveConnsNum++
					}
				}
			}
		}()
	}

	return conn, err
}

// PutConn 放回
func (p *Pools) PutConn(conn net.Conn) {

	if p.closed {
		conn.Close()
		return
	}

	adrr := conn.RemoteAddr().String()
	if _, ok := p.connPools[adrr]; !ok {
		// conn对应的连接池不存在，说明已经被关闭
		conn.Close()
		return
	}
	err := p.connPools[adrr].Put(conn)
	//不管成功与否，更新时间
	p.lastUsedTime[adrr] = time.Now()

	if err != ErrPoolFull {

		//如果是空闲连接，池满直接关闭
		if _, ok := p.idleConnsNum[adrr]; ok {
			p.idleConnsNum[adrr]--
			conn.Close()
		}

		// 如果是活跃连接，放入异步队列
		// 如果连接已经超时，则关闭连接并尝试重新建立连接
		pc := p.connPools[conn.RemoteAddr().String()]
		if time.Now().Sub(conn.(*poolConn).lastUsedTime) > pc.idleTimeout {
			conn.Close()
			return
		}

		a := []interface{}{p, conn}
		p.retryPutList <- a

	} else {
		p.lastUsedTime[adrr] = time.Now()
	}
	return
}

// Close 关闭所有连接池
func (p *Pools) Close() error {
	if p.closed {
		return ErrPoolClosed
	}

	p.closeLock.Lock()
	defer p.closeLock.Unlock()
	lenpool := len(p.connPools)

	closeWaitGroup := sync.WaitGroup{}
	closeWaitGroup.Add(lenpool)

	for k, v := range p.connPools {
		go func(k string, v *ConnectionPool) {
			defer closeWaitGroup.Done()

			err := p.connPools[k].ClosePool()
			if err != nil || p.connPools[k].closed != true {
				// 删除失败,加入重试队列  不能放adrr  要直接放 *ConnectionPool
				p.retryDelList <- v
			} else {
				// 删除成功

				// SetFinalizer
				runtime.SetFinalizer(p.connPools[k], p.connPools[k].Finalizer)

				delete(p.connPools, k)
			}
		}(k, v)
	}

	closeWaitGroup.Wait()

	p.closed = true
	close(p.checkIdleStopChan)
	close(p.retryNewStopChan)
	close(p.retryDelStopChan)
	close(p.poolClosedChan)
	close(p.retryPutStopChan)

	p.activeConnsNum = nil
	p.activeConnsNum = nil
	p.connPools = nil

	runtime.GC()

	return nil
}

// ClosePool 当独关闭一个连接池
func (p *Pools) ClosePool(adrr string) error {
	if p.closed {
		return ErrPoolClosed
	}
	if _, ok := p.connPools[adrr]; !ok {
		return ErrPoolNotExist
	}

	// 统计连接
	p.connNum[adrr] -= p.connPools[adrr].currConns
	if _, ok := p.activeConnsNum[adrr]; ok {
		delete(p.activeConnsNum, adrr)
	}
	if _, ok := p.idleConnsNum[adrr]; ok {
		delete(p.idleConnsNum, adrr)
	}

	err := p.connPools[adrr].ClosePool()

	if err == nil {
		delete(p.connPools, adrr)
	} else {
		return err
	}
	return nil
}

//	active -> idle
//
// idlecheck(超过一段时间 time.now()> p.idleTimeout+poolconn.lastusedtime没用就active变成idle)
func (p *Pools) idlecheck() {
	ticker := time.NewTicker(p.idleCheckFreq)
	defer ticker.Stop()

	for {

		select {
		// 检查连接池中所有连接的超时状态
		case <-ticker.C:
			p.idlecheckLock.RLock()

			// 连接池已关闭
			if p.closed {
				p.idlecheckLock.RUnlock()
				continue
			}

			// 检查所以adrr连接的使用频率
			for k, v := range p.connPools {
				go func(addr string, cp *ConnectionPool) {
					cp.idlecheckLock.RLock()
					defer cp.idlecheckLock.RUnlock()

					if _, ok := p.idleConnsNum[addr]; ok {
						// 如果是活跃连接

						now := time.Now()
						if now.Sub(cp.lastTime()) > cp.idleTimeout {
							// 该addrr连接最新的已经超时

							p.curActiveConnsNum--
							delete(p.activeConnsNum, addr)
							p.curIdleConnsNum++
							p.idleConnsNum[addr] = p.activeThreshold / cp.poolNum

							// 通知更新连接
							cp.keepAliveChan <- struct{}{}

						} else if cp.currConns < p.activeThreshold {
							// 该活跃连接使用已经降低

							p.curActiveConnsNum--
							delete(p.activeConnsNum, addr)
							p.curIdleConnsNum++
							p.idleConnsNum[addr] = cp.currConns
						}

					} else {
						// 如果是空闲连接
						now := time.Now()
						if now.Sub(cp.lastTime()) > cp.idleTimeout {
							// 该连接超时

							// 通知更新连接
							cp.keepAliveChan <- struct{}{}
						} else if cp.currConns >= p.activeThreshold {
							// 该连接使用频率提升
							p.curActiveConnsNum--
							delete(p.activeConnsNum, addr)
							p.curIdleConnsNum++
							p.idleConnsNum[addr] = cp.currConns
						}
					}
				}(k, v)

			}

			//活跃连接太多通知扩容
			if p.curActiveConnsNum >= p.maxActiveConnsNum {
				go p.checkConns(1)
			}
			//空闲连接太多通知缩容
			if p.curIdleConnsNum >= p.maxIdleConnsNum {
				go p.checkConns(-1)
			}

			//不用waitgroup  加快速度
			p.idlecheckLock.RUnlock()

		//连接池关闭
		case <-p.checkIdleStopChan:
			return

		}
	}
	//是空闲连接

}

// 限制最连接数
func (p *Pools) checkConns(mod int) {
	// TODO 对lastusedtime排序，再对使用频率靠前的expand   靠后的shrink

	if mod >= 0 {
		//扩容
		for k := range p.activeConnsNum {
			go func(addr string, cp *ConnectionPool) {
				if cp.currConns <= cp.shrinkNum {
					go cp.expand()
					// 不要等待返回结果，虽然有误差，但是快一些，等下次idlecheck值会最终正确
					p.activeConnsNum[addr] = cp.maxConns - cp.shrinkNum/cp.poolNum
				}

			}(k, p.connPools[k])
		}

	} else {
		//shrink
		for k := range p.idleConnsNum {
			go func(addr string, cp *ConnectionPool) {
				if cp.currConns > cp.shrinkNum {
					go cp.shrink()
					// 不要等待返回结果，虽然有误差，但是快一些，等下次idlecheck值会最终正确
					p.idleConnsNum[addr] = cp.shrinkNum
				}
			}(k, p.connPools[k])
		}

	}
}

// retry 重试新建连接池
func (p *Pools) retryNew() {
	for {
		select {
		case op := <-p.retryNewList:
			go func(op *PoolOptions) {
				if _, ok := p.connPools[op.adrr]; ok {
					//如果此时addr对应的连接池已经被创建了
					return
				}
				cp, err := NewConnectionPool(op.newConnFunc, op)
				if err != nil {
					// // 加入异步重试队列
					// p.retryList <- op
					return
				}
				p.connPools[op.adrr] = cp
			}(op)
		case <-p.retryNewStopChan:
			close(p.retryNewList)
			return
		}
	}
}

// retry 重试删除连接池
func (p *Pools) retryDelete() {
	for {
		select {
		case cp := <-p.retryDelList:
			go func(cp *ConnectionPool) {

				err := cp.ClosePool()
				if err != nil {
					// // 加入异步重试队列
					// p.retryList <- op
					return
				}
				// 批量删除，此时 p.connPools 已经被回收 不需要 delete(p.connPools, cp.adrr)
			}(cp)
		case <-p.retryDelStopChan:
			close(p.retryDelList)
			return

		}
	}
}

// retry 重试删除连接池
func (p *Pools) retryPut() {
	for {
		select {
		case rp := <-p.retryPutList:
			go func(p *Pools, c net.Conn) {
				adrr := c.RemoteAddr().String()
				err := p.connPools[adrr].Put(c)
				// 出错，删除
				if err != nil {
					c.Close()
					//判断此时是否为空闲连接
					if p.connPools[adrr].currConns < p.activeThreshold {
						delete(p.activeConnsNum, adrr)
					} else {
						p.activeConnsNum[adrr]--
					}
					return
				}
			}(rp[0].(*Pools), rp[1].(net.Conn))
		case <-p.retryPutStopChan:
			close(p.retryPutList)
			return
		}
	}
}
