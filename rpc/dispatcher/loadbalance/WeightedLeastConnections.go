package loadbalance

import (
	"container/heap"
	"errors"
)

// 使用优先级队列（Priority Queue）
// WeightedConnectionNode 代表具有加权连接数的节点
type WeightedConnectionNode struct {
	Node            string
	WeightedConnect int // 加权连接数
}

// PriorityQueueWeightedConnection 实现了优先级队列
type PriorityQueueWeightedConnection []*WeightedConnectionNode

func (pq PriorityQueueWeightedConnection) Len() int { return len(pq) }
func (pq PriorityQueueWeightedConnection) Less(i, j int) bool {
	return pq[i].WeightedConnect > pq[j].WeightedConnect
}
func (pq PriorityQueueWeightedConnection) Swap(i, j int) { pq[i], pq[j] = pq[j], pq[i] }

func (pq *PriorityQueueWeightedConnection) Push(x interface{}) {
	item := x.(*WeightedConnectionNode)
	*pq = append(*pq, item)
}

func (pq *PriorityQueueWeightedConnection) Pop() interface{} {
	old := *pq
	n := len(old)
	item := old[n-1]
	*pq = old[0 : n-1]
	return item
}

// WeightedLowConnectionBalancer 实现了加权最少连接负载均衡器
type WeightedLowConnectionBalancer struct {
	pq PriorityQueueWeightedConnection
}

// NewWeightedLowConnectionBalancer 创建一个新的加权最少连接负载均衡器实例
func NewWeightedLowConnectionBalancer() *WeightedLowConnectionBalancer {
	return &WeightedLowConnectionBalancer{
		pq: make(PriorityQueueWeightedConnection, 0),
	}
}

// Add 添加一个或多个节点到负载均衡器
func (b *WeightedLowConnectionBalancer) Add(nodes ...string) error {
	for _, node := range nodes {
		b.add(node, 1) // 默认权重为1
	}
	return nil
}

// add 添加一个节点到负载均衡器
func (b *WeightedLowConnectionBalancer) add(node string, weightedConnection int) {
	heap.Push(&b.pq, &WeightedConnectionNode{Node: node, WeightedConnect: weightedConnection})
}

// Get 返回加权连接数最少的节点
func (b *WeightedLowConnectionBalancer) Get() (string, error) {
	if b.pq.Len() == 0 {
		return "", errors.New("no nodes available")
	}
	node := heap.Pop(&b.pq).(*WeightedConnectionNode)
	return node.Node, nil
}
