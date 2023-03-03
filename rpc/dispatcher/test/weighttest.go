package testBalance

import (
	"fmt"
	"rpc_service/dispatcher/loadbalance"
)

func testwt() {
	weightLb := loadbalance.LoadBalanceFactory(loadbalance.Weight)
	weightLb.Add("a", "1")
	weightLb.Add("b", "2")
	weightLb.Add("c", "5")
	weightLb.Add("d", "2")
	weightLb.Add("e", "5")
	weightLb.Add("f", "1")

	var count = make(map[string]int)
	for i := 0; i < 50; i++ {
		weightRs, _ := weightLb.Get()
		count[weightRs]++
	}
	fmt.Println(count)

}
