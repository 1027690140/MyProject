package loadbalance

// LoadBalance 负载均衡器接口。
type LoadBalance interface {
	Add(...string) error
	Get() (string, error)
}
