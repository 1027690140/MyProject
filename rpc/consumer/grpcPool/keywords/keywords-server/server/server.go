package server

import (
	"context"
	"fmt"
	"rpc_service/consumer/grpcPool/keywords/proto"
)

type KwServer struct {
	proto.UnimplementedKeyWordsMatchServer
}

func (k KwServer) Match(ctx context.Context, request *proto.MatchRequest) (*proto.MatchResponse, error) {
	fmt.Printf("%+v\n", request)
	// 我们直接认为没有敏感词，直接返回true，敏感词为空
	return &proto.MatchResponse{
		Ok:   true,
		Word: "",
	}, nil
}
