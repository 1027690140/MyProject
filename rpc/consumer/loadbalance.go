package consumer

import (
	"fmt"
	lb "rpc_service/dispatcher/loadbalance"
)

type LoadBalance interface {
	Add(...string) error
	Get() (string, error)
}

type LoadBalanceMode int

const (
	RandomBalance LoadBalanceMode = iota
	RoundRobinBalance
	WeightRoundRobinBalance
	HashBalance
	HashBalanceSimple
	randomWeightedBalancer
)

type LoadBalanceChoice struct {
	Hash     func(date []byte) uint32
	id       int
	weight   int
	replicas int
}

func NewLoadBalanceChoice(mapparms map[string]interface{}) *LoadBalanceChoice {
	if len(mapparms) == 0 {
		return &LoadBalanceChoice{}
	}
	var id int
	var weight int
	var replicas int
	var Hash func(date []byte) uint32

	if v, ok := mapparms["id"]; ok {
		id = v.(int)
	}
	if v, ok := mapparms["weight"]; ok {
		weight = v.(int)
	}
	if v, ok := mapparms["replicas"]; ok {
		replicas = v.(int)
	}
	if v, ok := mapparms["Hash"]; ok {
		Hash = v.(func(date []byte) uint32)
	}
	return &LoadBalanceChoice{
		id:       id,
		weight:   weight,
		replicas: replicas,
		Hash:     Hash,
	}
}
func LoadBalanceFactory(mode LoadBalanceMode, servers []string, choice interface{}) (LoadBalance, error) {
	switch mode {
	case RandomBalance:
		return lb.NewRandomBalance(servers), nil
	case RoundRobinBalance:
		return lb.NewRoundRobinBalance(servers), nil
	case WeightRoundRobinBalance:
		if choice == nil {
			return nil, fmt.Errorf("expect pamars LoadBalanceChoice")
		}
		if lc, ok := choice.(LoadBalanceChoice); ok {
			return lb.NewWeightRoundRobinBalance(servers, lc.id, lc.weight), nil
		}
		return nil, fmt.Errorf("expect pamars LoadBalanceChoice")

	case HashBalance:
		return lb.NewHashBalancer(), nil
	case HashBalanceSimple:
		if choice == nil {
			return nil, fmt.Errorf("expect pamars ")
		}
		if lc, ok := choice.(LoadBalanceChoice); ok {
			return lb.NewConsistentHashBanlance(lc.replicas, lc.Hash), nil
		}
		return nil, fmt.Errorf("expect pamars")
	case randomWeightedBalancer:
		if choice == nil {
			return nil, fmt.Errorf("missing parameters")
		}
		if lc, ok := choice.(*LoadBalanceChoice); ok {
			if lc.weight == 0 {
				return nil, fmt.Errorf("weight cannot be 0")
			}
			if lc.replicas == 0 {
				return nil, fmt.Errorf("call limit cannot be 0 调用次数限制不能为0")
			}
			return lb.NewRandomWeightedBalancer(servers, []int{lc.weight}, lc.replicas), nil
		}
		return nil, fmt.Errorf("incorrect parameter type")
	default:
		return lb.NewRandomBalance(servers), nil
	}
}
