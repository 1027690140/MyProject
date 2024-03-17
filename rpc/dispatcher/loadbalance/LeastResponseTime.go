package loadbalance

import (
	"container/heap"
	"errors"
	"strconv"
)

// 使用最小堆（Min Heap）：将节点的响应时间作为堆的关键字，每次从堆顶选择响应时间最小的节点。

// MinHeapResponseTime 实现了最小堆
type MinHeapResponseTime []*ResponseTimeNode

// ResponseTimeNode 代表具有响应时间的节点
type ResponseTimeNode struct {
	Node         string
	ResponseTime int // 响应时间
}

// LowResponseTimeBalancer 实现了最低响应时间优先负载均衡器
type LowResponseTimeBalancer struct {
	heap MinHeapResponseTime
}

// NewLowResponseTimeBalancer 创建一个新的最低响应时间优先负载均衡器实例
func NewLowResponseTimeBalancer() *LowResponseTimeBalancer {
	return &LowResponseTimeBalancer{
		heap: make(MinHeapResponseTime, 0),
	}
}

// Add 添加一个或多个节点到负载均衡器
func (b *LowResponseTimeBalancer) Add(nodes ...string) error {
	for _, node := range nodes {
		responseTime, err := strconv.Atoi(node)
		if err != nil {
			return errors.New("invalid response time")
		}
		heap.Push(&b.heap, &ResponseTimeNode{Node: node, ResponseTime: responseTime})
	}
	return nil
}

// add 添加一个或多个节点到负载均衡器
func (b *LowResponseTimeBalancer) add(node string, responseTime int) {
	heap.Push(&b.heap, &ResponseTimeNode{Node: node, ResponseTime: responseTime})
}

// Get 返回最低响应时间的节点
func (b *LowResponseTimeBalancer) Get() (string, error) {
	if b.heap.Len() == 0 {
		return "", errors.New("no nodes available")
	}
	node := heap.Pop(&b.heap).(*ResponseTimeNode)
	return node.Node, nil
}
