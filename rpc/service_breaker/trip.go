package serviceBreaker

// when error occur, determine whether the breaker should be opened.  熔断策略
type TripStrategyFunc func(Metrics) bool

// according to consecutive fail 连续错误数
func ConsecutiveFailTripFunc(threshold uint64) TripStrategyFunc {
	return func(m Metrics) bool {
		return m.ConsecutiveFail >= threshold
	}
}

// according to fail   错误数
func FailTripFunc(threshold uint64) TripStrategyFunc {
	return func(m Metrics) bool {
		return m.CountFail >= threshold
	}
}

// according to fail rate  错误比例
func FailRateTripFunc(rate float64, minCalls uint64) TripStrategyFunc {
	return func(m Metrics) bool {
		var currRate float64
		if m.CountAll != 0 {
			currRate = float64(m.CountFail) / float64(m.CountAll)
		}

		return m.CountAll >= minCalls && currRate >= rate
	}
}

const (
	ConsecutiveFailTrip = iota + 1
	FailTrip
	FailRateTrip
)

// choose trip
func ChooseTrip(op *TripStrategyOption) TripStrategyFunc {
	switch op.Strategy {
	case ConsecutiveFailTrip:
		return ConsecutiveFailTripFunc(op.ConsecutiveFailThreshold)
	case FailTrip:
		return FailTripFunc(op.FailThreshold)
	case FailRateTrip:
		fallthrough
	default:
		return FailRateTripFunc(op.FailRate, op.MinCall)
	}
}
