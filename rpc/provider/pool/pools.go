package pool

import (
	"sync"
	"time"
)

// 全局pool 管理不同adrr 的pool  对所有的空闲、活跃连接进行管理
type Pools struct {
	poolNum int

	connPools map[string]*ConnectionPool //不同adrr对应的连接池

	activeThreshold int            // 活跃阈值 超过该值就是活跃的连接
	activeConnsNum  map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	connNum         map[string]int // 连接    统计所以连接以及对应的连接数

	activeConns map[string][]*poolConn // 当前活跃的连接
	idleConns   map[string][]*poolConn // 当前空闲的连接

	//--------------------
	connLastused map[string][]time.Time // 不同adrr对应的最后使用时间 用于缩容
	//--------------------

	lock            sync.RWMutex   // 锁
	expandingLock   sync.Mutex     // 扩容锁
	shrinkingLock   sync.Mutex     // 缩容锁
	idlecheckLock   sync.RWMutex   // 空闲连接检查锁
	activecheckLock sync.RWMutex   // 活跃连接检查锁
	stopWaitGroup   sync.WaitGroup // 关闭连接池等待组

	closed bool // 是否关闭
}
