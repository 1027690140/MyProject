package loadbalance

// LeastConnection算法=================

func (h MinHeapConnection) Len() int           { return len(h) }
func (h MinHeapConnection) Less(i, j int) bool { return h[i].Connection < h[j].Connection }
func (h MinHeapConnection) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MinHeapConnection) Push(x interface{}) {
	*h = append(*h, x.(*ConnectionNode))
}

func (h *MinHeapConnection) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// LeastResponseTime算法=================

func (h MinHeapResponseTime) Len() int           { return len(h) }
func (h MinHeapResponseTime) Less(i, j int) bool { return h[i].ResponseTime < h[j].ResponseTime }
func (h MinHeapResponseTime) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *MinHeapResponseTime) Push(x interface{}) {
	*h = append(*h, x.(*ResponseTimeNode))
}

func (h *MinHeapResponseTime) Pop() interface{} {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[0 : n-1]
	return item
}

// ovber算法=================

// Len 返回堆的长度。
func (h MaxConnectionHeap) Len() int { return len(h) }

// Less 比较堆中两个节点的连接数。
func (h MaxConnectionHeap) Less(i, j int) bool {
	return h[i].Connection > h[j].Connection
}

// Swap 交换堆中的两个节点。
func (h MaxConnectionHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push 向堆中添加一个节点。
func (h *MaxConnectionHeap) Push(x interface{}) {
	*h = append(*h, x.(*ObservedNode))
}

// Pop 从堆中弹出顶部节点。
func (h *MaxConnectionHeap) Pop() interface{} {
	old := *h
	n := len(old)
	node := old[n-1]
	*h = old[0 : n-1]
	return node
}

// MaxResponseTimeHeap 表示最大响应时间的堆。
type MaxResponseTimeHeap []*ObservedNode

// Len 返回堆的长度。
func (h MaxResponseTimeHeap) Len() int { return len(h) }

// Less 比较堆中两个节点的响应时间。
func (h MaxResponseTimeHeap) Less(i, j int) bool {
	return h[i].ResponseTime > h[j].ResponseTime
}

// Swap 交换堆中的两个节点。
func (h MaxResponseTimeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push 向堆中添加一个节点。
func (h *MaxResponseTimeHeap) Push(x interface{}) {
	*h = append(*h, x.(*ObservedNode))
}

// Pop 从堆中弹出顶部节点。
func (h *MaxResponseTimeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	node := old[n-1]
	*h = old[0 : n-1]
	return node
}

// MinResponseTimeHeap 表示最小响应时间的堆。
type MinResponseTimeHeap []*ObservedNode

// Len 返回堆的长度。
func (h MinResponseTimeHeap) Len() int { return len(h) }

// Less 比较堆中两个节点的响应时间。
func (h MinResponseTimeHeap) Less(i, j int) bool {
	return h[i].ResponseTime < h[j].ResponseTime
}

// Swap 交换堆中的两个节点。
func (h MinResponseTimeHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

// Push 向堆中添加一个节点。
func (h *MinResponseTimeHeap) Push(x interface{}) {
	*h = append(*h, x.(*ObservedNode))
}

// Pop 从堆中弹出顶部节点。
func (h *MinResponseTimeHeap) Pop() interface{} {
	old := *h
	n := len(old)
	node := old[n-1]
	*h = old[0 : n-1]
	return node
}
