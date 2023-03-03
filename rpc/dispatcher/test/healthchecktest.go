package testBalance

import (
	"fmt"
	"rpc_service/dispatcher/healthcheck"
	"time"
)

func testhk() {
	healthcheck.AddAddr("http://www.sina.com", "http://www.baidu.com", "http://www.aajklsdfjklsd")
	go healthcheck.HealthCheck()

	time.Sleep(50 * time.Second)

	alist := healthcheck.GetAliveAddrList()
	for i := 0; i < len(alist); i++ {
		fmt.Println(alist[i])
	}

	var block = make(chan bool)
	<-block

}
