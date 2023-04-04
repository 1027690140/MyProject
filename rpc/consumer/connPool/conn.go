package pool

import (
	"net"
	"time"
)

var _ net.Conn = (*poolConn)(nil)

// poolConn
type poolConn struct {
	conn         net.Conn
	pool         *ConnectionPool
	lastUsedTime time.Time
}

// Read 实现 net.Conn 接口
func (pc *poolConn) Read(b []byte) (n int, err error) {
	n, err = pc.conn.Read(b)
	pc.lastUsedTime = time.Now() // 更新最后使用时间
	return
}

// Write 实现 net.Conn 接口
func (pc *poolConn) Write(b []byte) (n int, err error) {
	n, err = pc.conn.Write(b)
	pc.lastUsedTime = time.Now() // 更新最后使用时间
	return
}

// Close 实现 net.Conn 接口
func (pc *poolConn) Close() error {
	return pc.pool.releaseConn(pc.conn)
}

// LocalAddr 实现 net.Conn 接口
func (pc *poolConn) LocalAddr() net.Addr {
	return pc.conn.LocalAddr()
}

// RemoteAddr 实现 net.Conn 接口
func (pc *poolConn) RemoteAddr() net.Addr {
	return pc.conn.RemoteAddr()
}

// SetDeadline 实现 net.Conn 接口
func (pc *poolConn) SetDeadline(t time.Time) error {
	return pc.conn.SetDeadline(t)
}

// SetReadDeadline 实现 net.Conn 接口
func (pc *poolConn) SetReadDeadline(t time.Time) error {
	return pc.conn.SetReadDeadline(t)
}

// SetWriteDeadline 实现 net.Conn 接口
func (pc *poolConn) SetWriteDeadline(t time.Time) error {
	return pc.conn.SetWriteDeadline(t)
}
