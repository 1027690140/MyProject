package pool

import (
	"net"
	"runtime"
	"sort"
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

// 全局pool 管理不同addr 的pool  对所有的空闲、活跃连接进行管理
type Pools struct {
	poolStoper      // chan to stop
	LRU        *LRU // lru
	// 下面的连接数 指的是addr种类数 类似set
	curActiveConnsNum int //当前活跃连接数
	curIdleConnsNum   int //当前空闲连接数  ： 即使用频率低于activeThreshold的连接
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	connPools map[string]*ConnectionPool //不同addr对应的连接池

	activeThreshold int // 活跃阈值 超过该值就是活跃的连接

	connNum        map[string]int // 连接  统计所以连接以及对应的连接数
	activeConnsNum map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	idleConnsNum   map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	lastUsedTime   map[string]time.Time
	idleCheckFreq  time.Duration // 空闲连接检查频率

	idlecheckLock sync.RWMutex // 空闲连接检查锁
	closeLock     sync.RWMutex // 关闭锁

	retryNewList chan *PoolOptions    // 重试新建队列
	retryDelList chan *ConnectionPool // 重试删除队列
	retryPutList chan []interface{}   // 重试put队列

	closed bool // 是否关闭
}

// NewPools return pools
func NewPools(option *PoolsParameter, opts ...*PoolOptions) (*Pools, error) {
	if len(opts) == 0 {
		return nil, ErrPoolsParameterNotExist
	}
	if option == nil {
		option = DefaultParameter
	}

	pool := &Pools{
		poolStoper: poolStoper{
			retryNewStopChan:  make(chan struct{}),
			retryDelStopChan:  make(chan struct{}),
			retryPutStopChan:  make(chan struct{}),
			checkIdleStopChan: make(chan struct{}),
			poolClosedChan:    make(chan struct{}),
		},
		LRU:               NewLRU(999999999, option.idleCheckFreq),
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
	// 初始化
	for i := 0; i < lens; i++ {
		go func(i int) {
			op := opts[i]
			cp, err := NewConnectionPool(op.newConnFunc, op)
			if err != nil {
				// 加入异步重试队列
				pool.retryNewList <- op
				return
			}
			pool.connPools[op.addr] = cp
		}(i)
	}

	// // 检查连接是否超时
	// go pool.KeepAliveCheck()

	// 检查空闲连接
	go pool.idlecheck()
	go pool.LRU.RunIdleCheck(option.idleCheckFreq)

	// 开启重试
	go pool.retryNew()
	go pool.retryDelete()
	go pool.retryPut()

	return pool, nil
}

// NewPoolConnection 新建单个连接池
func (p *Pools) NewPoolConnection(op *PoolOptions) error {
	if op.addr == "" {
		return ErrAddress
	}
	_, ok := p.connPools[op.addr]

	if ok {
		return nil
	}
	pc, err := NewConnectionPool(op.newConnFunc, op)
	if err != nil {
		return err
	}
	p.connPools[op.addr] = pc
	return nil
}

// GetConn 通过给定的addr 获取连接
func (p *Pools) GetConn(addr string) (net.Conn, error) {
	if p.closed {
		return nil, ErrPoolClosed
	}
	if _, ok := p.connPools[addr]; !ok {
		return nil, ErrPoolNotExist
	}

	// // 如果有空闲连接，则直接返回
	// if conn := p.popActiveConn(addr); conn != nil {
	// 	p.lastUsedTime[addr] = time.Now()
	// 	return conn, nil
	// }

	conn, err := p.connPools[addr].Get()
	if err != nil {
		return nil, err
	}

	// 更新时间
	p.lastUsedTime[addr] = time.Now()
	// 将连接池地址移动到链表头部
	if err == nil {
		// 统计连接数量
		p.connNum[addr] = p.connPools[addr].currConns

		// 由空闲连接 -> 活跃连接
		if p.connNum[addr] >= p.activeThreshold {
			p.activeConnsNum[addr] = p.connNum[addr]
			//如果原先是空闲连接
			if _, ok := p.idleConnsNum[addr]; ok {
				delete(p.activeConnsNum, addr)
				delete(p.idleConnsNum, addr)
				p.curIdleConnsNum--
				p.curActiveConnsNum++
			}
		}
	}
	p.LRU.Add(addr)
	return conn, err
}

// PutConn 放回
func (p *Pools) PutConn(conn net.Conn) {

	if p.closed {
		conn.Close()
		return
	}

	addr := conn.RemoteAddr().String()
	if _, ok := p.connPools[addr]; !ok {
		// conn对应的连接池不存在，说明已经被关闭
		conn.Close()
		return
	}
	err := p.connPools[addr].Put(conn)
	//不管成功与否，更新时间
	p.LRU.Add(addr)
	p.lastUsedTime[addr] = time.Now()

	if err != ErrPoolFull {

		//如果是空闲连接，池满直接关闭
		if _, ok := p.idleConnsNum[addr]; ok {
			p.idleConnsNum[addr]--
			conn.Close()
		}

		// 如果是活跃连接，放入异步队列
		// 如果连接已经超时，则关闭连接并尝试重新建立连接
		pc := p.connPools[conn.RemoteAddr().String()]
		if time.Now().Sub(conn.(*poolConn).lastUsedTime) > pc.idleTimeout {
			conn.Close()
			return
		}

		go func() {
			// 加入异步队列重试 go防止满了阻塞
			a := []interface{}{p, conn}
			p.retryPutList <- a
		}()

	} else {
		p.lastUsedTime[addr] = time.Now()
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
				go func() {
					// 删除失败,加入重试队列  不放addr  直接放 *ConnectionPool
					p.retryDelList <- v
				}()
			} else {
				// 删除成功

				// SetFinalizer
				runtime.SetFinalizer(p.connPools[k], p.connPools[k].Finalizer)
			}
			// 失败也delete，最终一致性
			delete(p.connPools, k)

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
func (p *Pools) ClosePool(addr string) error {
	if p.closed {
		return ErrPoolClosed
	}
	if _, ok := p.connPools[addr]; !ok {
		return ErrPoolNotExist
	}

	// 统计连接
	p.connNum[addr] -= p.connPools[addr].currConns
	if _, ok := p.activeConnsNum[addr]; ok {
		delete(p.activeConnsNum, addr)
	}
	if _, ok := p.idleConnsNum[addr]; ok {
		delete(p.idleConnsNum, addr)
	}

	err := p.connPools[addr].ClosePool()
	if err == nil {
		delete(p.connPools, addr)
	} else {
		return err
	}
	return nil
}

//	active -> idle
//
// idlecheck(超过一段时间 time.now()> p.idleTimeout+poolconn.lastusedtime 没用就active变成idle)
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
				return
			}

			// 检查所以addr连接的使用频率
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

func (p *Pools) sortTime(mod int, sortedTimeNew []interface{}) {
	if mod >= 0 {
		//  对lastusedtime排序，再对使用频率靠前的expand   靠后的shrink
		sortedTimeNew := make([]interface{}, len(p.lastUsedTime)/2)

		for k, v := range p.lastUsedTime {
			//对活跃连接排序  用于expan
			if _, ok := p.activeConnsNum[k]; ok {
				sortedTimeNew = append(sortedTimeNew, [2]interface{}{v, k})
			}
		}
		// 对 sortedTimeNew 按时间从新到旧排序
		sort.Slice(sortedTimeNew, func(i, j int) bool {
			return sortedTimeNew[i].([2]interface{})[0].(time.Time).After(sortedTimeNew[j].([2]interface{})[0].(time.Time))
		})
	} else {
		//  对lastusedtime排序，再对使用频率靠前的expand   靠后的shrink
		sortedTimeOld := make([]interface{}, len(p.lastUsedTime)/2)

		for k, v := range p.lastUsedTime {
			//对不活跃连接排序  用于shrink
			if _, ok := p.idleConnsNum[k]; ok {
				sortedTimeOld = append(sortedTimeOld, [2]interface{}{v, k})
			}
		}
		// 对sortedTimeOld 按时间从旧到新排序
		sort.Slice(sortedTimeOld, func(i, j int) bool {
			return sortedTimeOld[i].([2]interface{})[0].(time.Time).Before(sortedTimeOld[j].([2]interface{})[0].(time.Time))
		})
	}

}

// 限制最连接数
func (p *Pools) checkConns(mod int) {
	// //对lastusedtime排序，再对使用频率靠前的expand   靠后的shrink
	// sortedTime := make([]interface{}, len(p.lastUsedTime)/2)

	if mod >= 0 {
		//expand

		// p.sortTime(mod, sortedTime)
		addrs := p.LRU.GetOldest(p.maxActiveConnsNum - len(p.activeConnsNum))
		for i := 0; i <= p.maxActiveConnsNum-len(p.activeConnsNum) && i < len(addrs); i++ {
			k := addrs[i]
			go func(addr string, cp *ConnectionPool) {
				if cp.currConns <= cp.shrinkNum {
					go cp.expand()
					// 不要等待返回结果，虽然有误差，但是快一些，等 idlecheck值 最终正确
					p.activeConnsNum[addr] = cp.maxConns - cp.shrinkNum/cp.poolNum
				}

			}(k, p.connPools[k])
		}

	} else {
		//shrink

		// p.sortTime(mod, sortedTime)
		addr := p.LRU.GetNewest(p.maxIdleConnsNum - len(p.idleConnsNum))
		for i := 0; i <= p.maxIdleConnsNum-len(p.idleConnsNum) && i < len(addr); i++ {
			k := addr[i]
			go func(addr string, cp *ConnectionPool) {
				if cp.currConns > cp.shrinkNum {
					go cp.shrink()
					// 不要等待返回结果，虽然有误差，但是快一些，等 idlecheck值 最终正确
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
				if _, ok := p.connPools[op.addr]; ok {
					//如果此时addr对应的连接池已经被创建了
					return
				}
				cp, err := NewConnectionPool(op.newConnFunc, op)
				if err != nil {
					// // 加入异步重试队列
					// p.retryList <- op
					return
				}
				p.connPools[op.addr] = cp
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
				// 批量删除，此时 p.connPools 已经被回收 不需要 delete(p.connPools, cp.addr)
			}(cp)
		case <-p.retryDelStopChan:
			close(p.retryDelList)
			return

		}
	}
}

// retry 重试放回连接池
func (p *Pools) retryPut() {
	for {
		select {
		case rp := <-p.retryPutList:
			go func(p *Pools, c net.Conn) {
				addr := c.RemoteAddr().String()
				err := p.connPools[addr].Put(c)
				// 出错，删除
				if err != nil {
					c.Close()
					//判断此时是否为空闲连接
					if p.connPools[addr].currConns < p.activeThreshold {
						delete(p.activeConnsNum, addr)
					} else {
						p.activeConnsNum[addr]--
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
