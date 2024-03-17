package loadbalance

import (
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"
)

// RandomWeightedBalancer 表示随机加权负载均衡器。
type RandomWeightedBalancer struct {
	allNodes       []*WeightNode
	totalWeight    int
	currentWeights map[string]int
	callLimit      int
}

// NewRandomWeightedBalancer 创建一个新的随机加权负载均衡器实例。
func NewRandomWeightedBalancer(nodes []string, weights []int, callLimit int) *RandomWeightedBalancer {
	if len(nodes) != len(weights) || len(nodes) == 0 {
		fmt.Errorf("节点和权重长度需要相同，且至少需要一个节点")
		return nil
	}

	totalWeight := 0
	for _, weight := range weights {
		totalWeight += weight
	}

	return &RandomWeightedBalancer{
		allNodes:       createWeightedNodes(nodes, weights),
		totalWeight:    totalWeight,
		currentWeights: make(map[string]int),
		callLimit:      callLimit,
	}
}

// createWeightedNodes 创建带有给定节点和权重的 WeightNode 实例。
func createWeightedNodes(nodes []string, weights []int) []*WeightNode {
	weightedNodes := make([]*WeightNode, len(nodes))
	for i, node := range nodes {
		weightedNodes[i] = &WeightNode{
			node:          node,
			weight:        weights[i],
			currentWeight: 0,
		}
	}
	return weightedNodes
}

// Add 方法实现 LoadBalance 接口。
func (r *RandomWeightedBalancer) Add(params ...string) error {
	if len(params) != 2 {
		return errors.New("参数长度需要为2")
	}
	weight, err := strconv.ParseInt(params[1], 10, 64)
	if err != nil {
		return err
	}
	r.allNodes = append(r.allNodes, &WeightNode{
		node:          params[0],
		weight:        int(weight),
		currentWeight: 0,
	})
	return nil
}

// Get 方法实现 LoadBalance 接口。
func (r *RandomWeightedBalancer) Get() (string, error) {
	rand.NewSource(time.Now().UnixNano())
	randNum := rand.Intn(r.totalWeight) // 在 [0, totalWeight) 范围内生成一个随机数

	for _, weightedNode := range r.allNodes {
		r.currentWeights[weightedNode.node] += weightedNode.weight
		if r.currentWeights[weightedNode.node] > randNum {
			// 检查此节点的调用次数是否达到限制
			if r.currentWeights[weightedNode.node] >= r.callLimit {
				delete(r.currentWeights, weightedNode.node)
			}
			return weightedNode.node, nil
		}
	}

	return "", errors.New("没有可用的节点")
}
