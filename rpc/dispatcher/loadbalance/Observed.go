package loadbalance

import (
	"container/heap"
	"errors"
	"math"
)

// MaxConnectionHeap 表示最大连接数的堆。
type MaxConnectionHeap []*ObservedNode

// ObservedNode 表示观察算法负载均衡节点。
type ObservedNode struct {
	Node             string
	Connection       int     // 节点连接数
	ResponseTime     float64 // 节点响应时间
	ObservationScore float64 // 观分数
}

// ObservedBalance 表示观察算法负载均衡器。
type ObservedBalance struct {
	allNodes        map[string]*ObservedNode // 节点名及其信息
	minResponseTime float64                  // 最小响应时间
	maxResponseTime float64                  // 最大响应时间
}

// NewObservedBalance 创建一个新的观察算法负载均衡器实例。
func NewObservedBalance() *ObservedBalance {
	return &ObservedBalance{
		allNodes:        make(map[string]*ObservedNode),
		minResponseTime: math.MaxFloat64, // 初始化最小响应时间为最大值
		maxResponseTime: 0,               // 初始化最大响应时间为0
	}
}

// Add 添加节点到负载均衡器。
func (o *ObservedBalance) Add(nodes ...string) error {
	for _, node := range nodes {
		// 检查节点是否已存在
		if _, exists := o.allNodes[node]; exists {
			return errors.New("node already exists")
		}
		// 初始化节点信息
		o.allNodes[node] = &ObservedNode{
			Node:             node,
			Connection:       0,
			ResponseTime:     math.MaxFloat64, // 初始响应时间设为最大值
			ObservationScore: 0,
		}
	}
	return nil
}

// Update 更新节点的连接数和响应时间。
func (o *ObservedBalance) Update(node string, connection int, responseTime float64) {
	// 检查节点是否存在
	if _, exists := o.allNodes[node]; !exists {
		return // 节点不存在，无需更新
	}
	// 更新节点信息
	o.allNodes[node].Connection = connection
	o.allNodes[node].ResponseTime = responseTime
	// 更新最小响应时间
	if responseTime < o.minResponseTime {
		o.minResponseTime = responseTime
	}
	// 更新最大响应时间
	if responseTime > o.maxResponseTime {
		o.maxResponseTime = responseTime
	}
	// 计算观分数
	o.calculateObservationScore(node)
}

// calculateObservationScore 计算节点的观分数。
func (o *ObservedBalance) calculateObservationScore(node string) {
	// 获取节点信息
	n := o.allNodes[node]
	// 归一化连接数
	normalizedConnection := float64(n.Connection) / float64(o.getMaxConnection())
	// 归一化响应时间
	normalizedResponseTime := (n.ResponseTime - o.getMinResponseTime()) / (o.getMaxResponseTime() - o.getMinResponseTime())
	// 计算观分数
	n.ObservationScore = normalizedConnection + normalizedResponseTime
}

// getMaxConnection 获取最大连接数。
func (o *ObservedBalance) getMaxConnection() int {
	if len(o.allNodes) == 0 {
		return 0
	}

	maxConnectionHeap := make(MaxConnectionHeap, len(o.allNodes))
	i := 0
	for _, n := range o.allNodes {
		maxConnectionHeap[i] = n
		i++
	}
	heap.Init(&maxConnectionHeap)

	return maxConnectionHeap[0].Connection
}

// getMinResponseTime 获取最小响应时间。
func (o *ObservedBalance) getMinResponseTime() float64 {
	if len(o.allNodes) == 0 {
		return 0.0
	}

	minResponseTimeHeap := make(MinResponseTimeHeap, len(o.allNodes))
	i := 0
	for _, n := range o.allNodes {
		minResponseTimeHeap[i] = n
		i++
	}
	heap.Init(&minResponseTimeHeap)

	return minResponseTimeHeap[0].ResponseTime
}

// getMaxResponseTime 获取最大响应时间。
func (o *ObservedBalance) getMaxResponseTime() float64 {
	if len(o.allNodes) == 0 {
		return 0.0
	}

	maxResponseTimeHeap := make(MaxResponseTimeHeap, len(o.allNodes))
	i := 0
	for _, n := range o.allNodes {
		maxResponseTimeHeap[i] = n
		i++
	}
	heap.Init(&maxResponseTimeHeap)

	return maxResponseTimeHeap[0].ResponseTime
}

// Get 返回观察算法选择的节点。
func (o *ObservedBalance) Get() (string, error) {
	if len(o.allNodes) == 0 {
		return "", errors.New("no nodes available")
	}

	var selectedNode string
	maxObservationScore := -math.MaxFloat64

	// 找到观分数最高的节点
	for _, n := range o.allNodes {
		if n.ObservationScore > maxObservationScore {
			selectedNode = n.Node
			maxObservationScore = n.ObservationScore
		}
	}

	return selectedNode, nil
}
