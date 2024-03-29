package pool

import (
	"net"
	"time"
)

// const
const (
	DialTimeout = 5 * time.Second

	BackoffMaxDelay = 3 * time.Second

	KeepAliveTime = time.Duration(10) * time.Second

	KeepAliveTimeout = time.Duration(3) * time.Second

	InitialWindowSize = 1 << 30

	InitialConnWindowSize = 1 << 30

	MaxSendMsgSize = 4 << 30

	MaxRecvMsgSize = 4 << 30
)

// PoolOptions are params for creating grpc connect pool.
type PoolOptions struct {
	addr        string
	newConnFunc func(string) (net.Conn, error)

	minConns    int // 最小连接数
	maxConns    int // 最大连接数
	maxIdle     int // 最大空闲连接数
	currConns   int // 当前连接数 = 被取出的+还在池子里的
	poolNum     int // connections大小
	shrinkNum   int // 缩容数目
	connChanNum int //  []chan *poolConn 中每个chan大小

	indexFreq         time.Duration // 获取index超时时间
	idleCheckFreq     time.Duration // 空闲连接检查频率
	connectionTimeout time.Duration // 连接超时时间
	idleTimeout       time.Duration // 空闲连接超时时间
	keepAliveInterval time.Duration // 保活检查时间

	minShrinkInterval time.Duration // 最小缩容间隔
	minexpandInterval time.Duration // 最小扩容间隔

	maxLRUEntries int  // 最大LRU缓存数目
	reuse         bool // 是否复用连接
}

// DefaultPoolOptions sets a list of recommended options for good performance.
var DefaultPoolOptions = PoolOptions{
	minConns:          5,
	maxConns:          50,
	maxIdle:           20,
	poolNum:           5,
	shrinkNum:         20,
	connectionTimeout: 30 * time.Second, // 连接超时时间
	idleTimeout:       30 * time.Second, // 空闲连接超时时间
	keepAliveInterval: 30 * time.Second, // 保活检查时间
	reuse:             true,
}

type PoolsParameter struct {
	activeThreshold   int //活跃阈值 超过该值就是活跃的连接
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	idleCheckFreq     time.Duration // 空闲连接检查频率
	keepAliveInterval time.Duration // 保活检查时间

	maxLRUEntries int // 最大LRU缓存数目
}

var DefaultParameter = &PoolsParameter{
	activeThreshold:   5,
	maxActiveConnsNum: 50,
	maxIdleConnsNum:   20,

	idleCheckFreq:     30 * time.Second, // 空闲连接检查频率
	keepAliveInterval: 30 * time.Second, // 保活检查时间
}
