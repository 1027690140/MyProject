package client

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func (c *DefaultClient) getOptions() (opts []grpc.DialOption) {
	opts = make([]grpc.DialOption, 0)
	opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	return opts
}

// 不同的实现可能有不同的opts，比较复杂的时候，还可以考虑使用函数式选项模式
