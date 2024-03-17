package controllers

import (
	"context"
	"fmt"
	"rpc_service/consumer/grpcPool/grpcpool/services/keywords"
	kwProto "rpc_service/consumer/grpcPool/grpcpool/services/keywords/proto"
	"rpc_service/consumer/grpcPool/grpcpool/services/sensitive"
	sensitiveProto "rpc_service/consumer/grpcPool/grpcpool/services/sensitive/proto"

	"net/http"

	"github.com/gin-gonic/gin"
)

func Ping(ctx *gin.Context) {
	// 建立一个sensitive服务的客户端单例连接，并调用sensitive远程rpc服务的Validate接口
	spool := sensitive.GetSensitiveClientPool()
	sconn := spool.Get()
	// 注意用完后需要将连接手动放回连接池
	defer spool.Put(sconn)
	sensitiveClient := sensitiveProto.NewSensitiveFilterClient(sconn)
	sIn := &sensitiveProto.ValidateRequest{Input: "今天天气很好"}
	sensitiveRes, err := sensitiveClient.Validate(context.Background(), sIn)
	fmt.Printf("%+v    %+v  \n", sensitiveRes, err)

	// 建立一个keywords服务的客户端单例连接，并调用keywords远程rpc服务的Match接口
	kpool := keywords.GetKwClientPool()
	kconn := kpool.Get()
	// 注意用完后需要将连接手动放回连接池
	defer kpool.Put(kconn)
	keywordsClient := kwProto.NewKeyWordsMatchClient(kconn)
	kIn := &kwProto.MatchRequest{Input: "今天天气很好"}
	keywordsRes, err := keywordsClient.Match(context.Background(), kIn)
	fmt.Printf("%+v    %+v  \n", keywordsRes, err)

	ctx.JSON(http.StatusOK, gin.H{
		"message": "pong",
	})
}
