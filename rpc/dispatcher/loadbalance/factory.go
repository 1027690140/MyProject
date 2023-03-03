package loadbalance

const (
	Random = iota
	RoundRobin
	Weight
	HASH
)

func LoadBalanceFactory(lbType int) LoadBalance {
	switch lbType {
	case Random:
		return new(RandomBalancer)
	case RoundRobin:
		return new(RoundRobinBalancer)
	case Weight:
		return new(WeightRoundRobinBalancer)
	case HASH:
		return NewHashBalancer()
	default:
		return new(RoundRobinBalancer)
	}
}
