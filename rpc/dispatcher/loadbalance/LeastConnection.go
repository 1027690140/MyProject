package loadbalance

import (
	"container/heap"
	"errors"
	"strconv"
)

// 使用最小堆（Min Heap）：将节点的连接数作为堆的关键字，每次从堆顶选择连接数最少的节点。
// MinHeapConnection 实现了最小堆
type MinHeapConnection []*ConnectionNode

// ConnectionNode 代表具有连接数的节点
type ConnectionNode struct {
	Node       string
	Connection int // 连接数
}

// LowConnectionBalancer 实现了最少连接数负载均衡器
type LowConnectionBalancer struct {
	heap MinHeapConnection
}

// NewLowConnectionBalancer 创建一个新的最少连接数负载均衡器实例
func NewLowConnectionBalancer() *LowConnectionBalancer {
	return &LowConnectionBalancer{
		heap: make(MinHeapConnection, 0),
	}
}

// Add 添加一个或多个节点到负载均衡器
func (b *LowConnectionBalancer) add(node string, connection int) {
	heap.Push(&b.heap, &ConnectionNode{Node: node, Connection: connection})
}

// Add 添加一个节点到负载均衡器
func (b *LowConnectionBalancer) Add(nodes ...string) error {
	for i := 0; i < len(nodes); i += 2 {
		node := nodes[i]
		if i+1 >= len(nodes) {
			return errors.New("missing weight for node")
		}
		weightStr := nodes[i+1]
		weight, err := strconv.Atoi(weightStr)
		if err != nil {
			return errors.New("invalid weight")
		}
		heap.Push(&b.heap, &ConnectionNode{Node: node, Connection: weight})
	}
	return nil
}

// Get 返回连接数最少的节点
func (b *LowConnectionBalancer) Get() (string, error) {
	if b.heap.Len() == 0 {
		return "", errors.New("no nodes available")
	}
	node := heap.Pop(&b.heap).(*ConnectionNode)
	return node.Node, nil
}
