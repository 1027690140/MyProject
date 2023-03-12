package model

// conn the poll chan contains consumer.
type conn struct {
	ch         chan map[string]*InstanceInfo //
	instanch   chan *FetchData               //
	wait       chan struct{}                 // 出队通知
	arg        *ArgPolls
	latestTime int64
	count      int // cur count
	connOption *ConnOption
}

// newConn new consumer chan.
func newConn(ch chan map[string]*InstanceInfo, latestTime int64, arg *ArgPolls, connOption *ConnOption) (c *conn) {
	c = &conn{ch: ch, latestTime: latestTime, arg: arg, count: 1, connOption: connOption}
	return c
}

// ConnQueue is a queue for waited conn
type ConnQueue struct {
	headPos int
	head    []*conn
	tail    []*conn
}

func (q *ConnQueue) pushBack(w *conn) {
	q.tail = append(q.tail, w)
}

func (q *ConnQueue) popFront() *conn {
	if q.headPos >= len(q.head) {
		if len(q.tail) == 0 {
			return nil
		}
		// Pick up tail as new head, clear tail.
		q.head, q.headPos, q.tail = q.tail, 0, q.head[:0]
	}
	w := q.head[q.headPos]
	q.head[q.headPos] = nil
	q.headPos++
	w.wait <- struct{}{}
	return w
}
