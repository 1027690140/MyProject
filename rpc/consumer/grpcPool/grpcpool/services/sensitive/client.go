package sensitive

import (
	"rpc_service/consumer/grpcPool/grpcpool/services"
	"rpc_service/consumer/grpcPool/grpcpool/services/client"
	"sync"
)

//	注意是小写的，因为一个gin web服务，我们只希望它对一个grpc服务持有的连接池是一个单例
//
// 因此小写，避免其他地方可以构造这个结构体的对象。然后这里通过once控制是单例
type sensitiveClient struct {
	// 内嵌client.DefaultClient，从而实现了ServiceClient接口
	// 如果有其他实现，比如SensitiveClient ,那么内嵌SensitiveClient即可
	client.DefaultClient
}

var pool services.ClientPool
var once sync.Once

// 实际工作中，这里应该用服务的注册与发现机制，这里只是简单演示，所以写死了服务端的地址
var sensitiveAddr = "localhost:8001"

func GetSensitiveClientPool() services.ClientPool {
	once.Do(func() {
		c := &sensitiveClient{}
		// 实际调用的是内嵌的DefaultClient的GetPool
		pool = c.GetPool(sensitiveAddr)
	})
	return pool
}
