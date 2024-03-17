package services

import (
	"log"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/connectivity"
)

// 注意这里是大写开头，定义的是一个接口
type ClientPool interface {
	Get() *grpc.ClientConn
	Put(conn *grpc.ClientConn)
}

// 注意这里是小写开头，定义的是结构体，用于实现上面的ClientPool接口
type clientPool struct {
	pool sync.Pool
}

// 获取连接池对象，并定义新建连接的方法,返回ClientPool接口类型
func GetPool(target string, opts ...grpc.DialOption) (ClientPool, error) {
	return &clientPool{
		pool: sync.Pool{
			New: func() any {
				conn, err := grpc.Dial(target, opts...)
				if err != nil {
					log.Fatal(err)
				}
				return conn
			},
		},
	}, nil
}

// 从连接池中获取一个连接
func (c *clientPool) Get() *grpc.ClientConn {
	conn := c.pool.Get().(*grpc.ClientConn)

	// 当连接不可用时，关闭当前连接，并新建一个连接
	if conn.GetState() == connectivity.Shutdown || conn.GetState() == connectivity.TransientFailure {
		conn.Close()
		conn = c.pool.New().(*grpc.ClientConn)
	}
	return conn
}

// 与数据库连接池不太一样，数据库连接池一个连接用完了会自动返回池中
// 而sync.Pool中的连接用完了，需要我们手动的放回去，故提供一个Put方法
func (c *clientPool) Put(conn *grpc.ClientConn) {
	// 当连接不可用时，关闭当前连接，并不再放回池中
	if conn.GetState() == connectivity.Shutdown || conn.GetState() == connectivity.TransientFailure {
		conn.Close()
		return
	}

	c.pool.Put(conn)
}
