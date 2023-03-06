package pool

import (
	"net"
	"sync"
	"time"
)

// 全局pool 管理不同adrr 的pool  对所有的空闲、活跃连接进行管理
type Pools struct {
	connPools map[string]*ConnectionPool //不同adrr对应的连接池

	activeThreshold int // 活跃阈值 超过该值就是活跃的连接

	connNum        map[string]int         // 连接    统计所以连接以及对应的连接数
	activeConnsNum map[string]int         // 活跃连接 统计活跃连接以及对应的连接数
	activeConns    map[string][]*poolConn // 当前活跃的连接
	idleConnsNum   map[string]int         // 活跃连接 统计活跃连接以及对应的连接数
	idleConns      map[string][]*poolConn // 当前空闲的连接

	// 下面的连接数 指的是adrr种类数 类似set
	curActiveConnsNum int //当前活跃连接数
	curIdleConnsNum   int //当前空闲连接数
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	idleCheckFreq     time.Duration // 空闲连接检查频率
	keepAliveInterval time.Duration // 保活检查时间

	closeLock       sync.RWMutex // 关闭锁
	expandingLock   sync.Mutex   // 扩容锁
	shrinkingLock   sync.Mutex   // 缩容锁
	idlecheckLock   sync.RWMutex // 空闲连接检查锁
	activecheckLock sync.RWMutex // 活跃连接检查锁

	keepAliveStopChan chan struct{} //关闭keepalive
	checkIdleStopChan chan struct{} //关闭checkIdle
	retryStopChan     chan struct{} //关闭retry
	poolClosedChan    chan struct{} //通知关闭

	retryList chan *PoolOptions // 重试队列

	closed bool // 是否关闭
}

type PoolsParameter struct {
	activeThreshold   int //活跃阈值 超过该值就是活跃的连接
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	idleCheckFreq     time.Duration // 空闲连接检查频率
	keepAliveInterval time.Duration // 保活检查时间
}

// NewPools return pools
func NewPools(opts []*PoolOptions, option *PoolsParameter) (*Pools, error) {
	if len(opts) == 0 {
		return nil, ErrPoolsParameterNotExist
	}

	pool := &Pools{
		connPools:         make(map[string]*ConnectionPool),
		activeConnsNum:    make(map[string]int),
		idleConnsNum:      make(map[string]int),
		connNum:           make(map[string]int),
		idleConns:         make(map[string][]*poolConn),
		retryList:         make(chan *PoolOptions, 100),
		retryStopChan:     make(chan struct{}),
		idleCheckFreq:     option.idleCheckFreq,
		keepAliveInterval: option.keepAliveInterval,
		activeThreshold:   option.activeThreshold,
		maxActiveConnsNum: option.maxActiveConnsNum,
		maxIdleConnsNum:   option.maxIdleConnsNum,
		curActiveConnsNum: 0,
		curIdleConnsNum:   0,
	}
	lens := len(opts)
	for i := 0; i < lens; i++ {
		go func() {
			op := opts[i]
			cp, err := NewConnectionPool(op.newConnFunc, op)
			if err != nil {
				// 加入异步重试队列
				pool.retryList <- op
				return
			}
			pool.connPools[op.adrr] = cp
		}()
	}

	// 检查连接是否超时
	go pool.KeepAliveCheck()

	// 检查空闲连接
	go pool.checkIdleConns()

	// 开启重试
	go pool.retryNew()

	return pool, nil
}

// NewPoolConnection 新建单个连接池
func (p *Pools) NewPoolConnection(op *PoolOptions) error {
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
	conn, err := p.connPools[adrr].Get()
	if err == nil {
		// 统计连接
		go func() {
			if err == nil {
				pc := p.connPools[adrr].wrapConn(conn).(*poolConn)
				// 统计连接数量
				p.connNum[adrr] = p.connPools[adrr].currConns

				// 由空闲连接 -> 活跃连接
				if p.connNum[adrr] >= p.activeThreshold {
					p.activeConns[adrr] = append(p.activeConns[adrr], pc)
					p.activeConnsNum[adrr] = p.connNum[adrr]
					//如果原先是空闲连接
					if _, ok := p.idleConns[adrr]; ok {
						delete(p.activeConnsNum, adrr)
						delete(p.idleConns, adrr)
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
func (p *Pools) PutConn(conn net.Conn) error {

	if p.closed {
		conn.Close()
		return nil
	}

	adrr := conn.RemoteAddr().String()
	if _, ok := p.connPools[adrr]; !ok {
		// conn对应的连接池不存在，说明已经被关闭
		conn.Close()
		return ErrPoolNotExist
	}
	return p.connPools[adrr].Put(conn)
}

// Close 关闭连接池
func (p *Pools) Close() error {
	if p.closed {
		return nil
	}

	p.closeLock.Lock()
	defer p.closeLock.Unlock()
	lenpool := len(p.connPools)

	closeWaitGroup := sync.WaitGroup{}
	closeWaitGroup.Add(lenpool)

	for k := range p.connPools {
		go func() {
			defer closeWaitGroup.Done()
			p.connPools[k].ClosePool()
		}()
	}

	closeWaitGroup.Wait()

	// idleConns  activeConns TODO

	p.closed = true
	close(p.keepAliveStopChan)
	close(p.checkIdleStopChan)
	close(p.retryStopChan)
	close(p.poolClosedChan)
	return nil
}

// ClosePool 当独关闭一个连接池
func (p *Pools) ClosePool(adrr string) {
	if p.closed {
		return
	}
	if _, ok := p.connPools[adrr]; !ok {
		return
	}

	// 统计连接
	p.connNum[adrr] -= p.connPools[adrr].currConns
	if _, ok := p.activeConns[adrr]; ok {
		delete(p.activeConns, adrr)
		delete(p.activeConnsNum, adrr)
	}
	if _, ok := p.idleConns[adrr]; ok {
		delete(p.idleConns, adrr)
		delete(p.idleConnsNum, adrr)
	}

	p.connPools[adrr].ClosePool()

}

// TODO    keepalive   idlecheck(超过一段时间 time.now()> p.idleTimeout+poolconn.lastusedtime没用就active变成idle)

func (p *Pools) idlecheck() {
	//是空闲连接
	p.idleConns[adrr] = append(p.idleConns[adrr], pc)
	p.idleConnsNum[adrr] = p.connNum[adrr]
	p.curIdleConnsNum++
}

// 限制最大空闲连接数
func (p *ConnectionPool) checkIdleConns() {
	ticker := time.NewTicker(p.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			p.idlecheckLock.Lock()
			if len(p.idleConns) > p.maxIdle {
				// 也可以改 p.activeConns 所以连接放在 p.activeConns 里 ，不用p.idleConns
				numConnsToClose := len(p.idleConns) - p.maxIdle
				for i := 0; i < numConnsToClose; i++ {
					for _, conn := range p.idleConns {
						p.releaseConn(conn)
					}
				}
			}
			p.idlecheckLock.Unlock()
		}
	}
}

// popActiveConn弹出一个空闲连接，如果没有空闲连接则返回
func (p *ConnectionPool) popActiveConn() *poolConn {
	p.lock.Lock()
	defer p.lock.Unlock()

	//如果给定adrr 遍历对应的conn
	// for _, conn := range p.connMap[adrress]

	for _, conn := range p.activeConns {
		if err := p.keepAlive(conn); err != nil {
			_ = conn.Close()
			p.currConns--
			continue
		}
		return conn
	}
	return nil
}

// retry 重试新建连接池
func (p *Pools) retryNew() {
	for {
		select {
		case op := <-p.retryList:
			go func() {
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
			}()
		case <-p.retryStopChan:
			return
		}
	}
}
