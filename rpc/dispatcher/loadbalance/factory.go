package loadbalance

const (
	Random = iota
	RoundRobin
	Weight
	HASH
	LeastConnection
	LeastResponseTime
	WeightedLeastConnections
	WeightedResponseTime
	Observed
)

func LoadBalanceFactory(lbType int) LoadBalance {
	switch lbType {
	case Random:
		return new(RandomBalancer)
	case RoundRobin:
		return new(RoundRobinBalancer)
	case Weight:
		return new(WeightRoundRobinBalancer)
	case LeastConnection:
		return NewLowConnectionBalancer()
	case LeastResponseTime:
		return NewLowResponseTimeBalancer()
	case WeightedLeastConnections:
		return NewWeightedLowConnectionBalancer()
	case WeightedResponseTime:
		return NewWeightedResponseTimeBalance()
	case Observed:
		return NewObservedBalance()
	default:
		return new(RoundRobinBalancer)
	}
}
