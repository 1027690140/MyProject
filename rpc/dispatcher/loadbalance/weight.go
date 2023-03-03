package loadbalance

import (
	"errors"
	"fmt"
	"strconv"
)

/*
每个节点设置对应的权重，权重越大可能被选中的次数越高，
某节点被选中的次数≈(本节点权重/全部权重) * 总分配次数。
*/

// node list
type WeightRoundRobinBalancer struct {
	curIdx   int
	allNodes []*WeightNode
}

// weight node
type WeightNode struct {
	node          string
	weight        int //init weight
	currentWeight int // 代表每次请求节点的当前权重，为currentWeight+weight
}

func NewWeightRoundRobinBalance(nodes []string, id int, weigth int) *WeightRoundRobinBalancer {
	if len(nodes) == 0 {
		fmt.Errorf("expect pamars %s", nodes)
		return nil

	}
	wh := make([]*WeightNode, len(nodes))
	for i := range nodes {
		wh[i].node = nodes[i]
		wh[i].weight = weigth
	}
	return &WeightRoundRobinBalancer{
		curIdx:   id,
		allNodes: wh,
	}
}

// add node
func (r *WeightRoundRobinBalancer) Add(params ...string) error {
	if len(params) != 2 {
		return errors.New("param len need 2")
	}
	parInt, err := strconv.ParseInt(params[1], 10, 64)
	if err != nil {
		return err
	}
	node := &WeightNode{node: params[0], weight: int(parInt)}
	r.allNodes = append(r.allNodes, node)
	return nil
}

// get node
func (r *WeightRoundRobinBalancer) Get() (string, error) {
	totalWeight := 0 //代表所有节点初始权重之和
	var bestNode *WeightNode
	for i := 0; i < len(r.allNodes); i++ {
		curNode := r.allNodes[i]
		totalWeight += curNode.weight
		curNode.currentWeight += curNode.weight

		//choose the largest weight
		if bestNode == nil || curNode.currentWeight > bestNode.currentWeight {
			bestNode = curNode
		}
	}
	if bestNode == nil {
		return "", errors.New("get error")
	}
	bestNode.currentWeight -= totalWeight
	return bestNode.node, nil
}
