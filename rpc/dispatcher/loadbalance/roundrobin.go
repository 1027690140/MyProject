package loadbalance

import "errors"

// list store all the node
type RoundRobinBalancer struct {
	curIdx   int
	allNodes []string
}

func NewRoundRobinBalance(nodes []string) LoadBalance {
	return &RoundRobinBalancer{allNodes: nodes}
}

// add node
func (r *RoundRobinBalancer) Add(params ...string) error {
	if len(params) == 0 {
		return errors.New("param len 1 at least")
	}
	r.allNodes = append(r.allNodes, params[0])
	return nil
}

// get node
func (r *RoundRobinBalancer) Get() (string, error) {
	if len(r.allNodes) == 0 {
		return "", errors.New("addr list is empty")
	}
	lens := len(r.allNodes)
	//move to beginning node
	if r.curIdx >= lens {
		r.curIdx = 0
	}
	curNode := r.allNodes[r.curIdx]
	//move to next node
	r.curIdx = (r.curIdx + 1) % lens
	return curNode, nil
}
