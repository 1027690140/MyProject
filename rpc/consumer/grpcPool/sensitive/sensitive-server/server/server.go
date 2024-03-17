package server

import (
	"context"
	"fmt"
	"rpc_service/consumer/grpcPool/sensitive/proto"
)

type SensitiveServer struct {
	proto.UnimplementedSensitiveFilterServer
}

func (s SensitiveServer) Validate(ctx context.Context, request *proto.ValidateRequest) (*proto.ValidateResponse, error) {
	fmt.Printf("%+v\n", request)
	// 我们直接认为没有敏感词，直接返回true，敏感词为空
	return &proto.ValidateResponse{
		Ok:   true,
		Word: "",
	}, nil
}
