package client

import (
	"log"
	"rpc_service/consumer/grpcPool/grpcpool/services"
)

type ServiceClient interface {
	GetPool(addr string) services.ClientPool
}

type DefaultClient struct {
}

func (c *DefaultClient) GetPool(addr string) services.ClientPool {
	pool, err := services.GetPool(addr, c.getOptions()...)
	if err != nil {
		log.Fatal(err)
	}
	return pool
}

// 还可以有很多其他的实现，比如KeywordsClient，SensitiveClient等，这里为了简单，就只写了DefaultClient
