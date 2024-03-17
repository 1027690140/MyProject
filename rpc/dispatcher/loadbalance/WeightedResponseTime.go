package loadbalance

import (
	"container/heap"
	"errors"
	"math"
)

// WeightedResponseTimeBalance 表示加权最短响应时间负载均衡器。
type WeightedResponseTimeBalance struct {
	pq PriorityQueue // 使用优先级队列存储节点
}

// WeightedResponseTimeNode 表示加权最短响应时间负载均衡节点。
type WeightedResponseTimeNode struct {
	node         string  // 节点名称
	responseTime float64 // 节点响应时间
	weight       int     // 节点权重
}

// NewWeightedResponseTimeBalance 创建一个新的加权最短响应时间负载均衡器实例。
func NewWeightedResponseTimeBalance() *WeightedResponseTimeBalance {
	return &WeightedResponseTimeBalance{
		pq: make(PriorityQueue, 0),
	}
}

// Add 添加节点到负载均衡器。
func (w *WeightedResponseTimeBalance) Add(nodes ...string) error {
	for _, node := range nodes {
		// 默认响应时间为最大值
		heap.Push(&w.pq, &WeightedResponseTimeNode{node: node, responseTime: math.MaxFloat64, weight: 1})
	}
	return nil
}

// Get 返回加权最短响应时间算法选择的节点。
func (w *WeightedResponseTimeBalance) Get() (string, error) {
	if len(w.pq) == 0 {
		return "", errors.New("no nodes available")
	}

	node := heap.Pop(&w.pq).(*WeightedResponseTimeNode)
	return node.node, nil
}

// PriorityQueue 优先级队列。
type PriorityQueue []*WeightedResponseTimeNode

// Len 返回队列长度。
func (pq PriorityQueue) Len() int { return len(pq) }

// Less 比较两个节点的响应时间。
func (pq PriorityQueue) Less(i, j int) bool {
	// 响应时间的倒数用于比较，响应时间越短，倒数越大
	return 1/pq[i].responseTime*float64(pq[i].weight) > 1/pq[j].responseTime*float64(pq[j].weight)
}

// Swap 交换两个节点。
func (pq PriorityQueue) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }

// Push 添加节点到队列。
func (pq *PriorityQueue) Push(x interface{}) {
	item := x.(*WeightedResponseTimeNode)
	*pq = append(*pq, item)
}

// Pop 弹出队列顶部节点。
func (pq *PriorityQueue) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}
