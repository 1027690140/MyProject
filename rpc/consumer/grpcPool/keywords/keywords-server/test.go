package test

import (
	"flag"
	"fmt"
	"log"
	"net"
	"rpc_service/consumer/grpcPool/keywords/keywords-server/server"
	"rpc_service/consumer/grpcPool/keywords/proto"

	"google.golang.org/grpc"
)

var (
	port = flag.Int("port", 50052, "")
)

func main() {
	flag.Parse()
	// 监听端口
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", *port))
	if err != nil {
		log.Fatal(err)
	}

	// 建立rpc服务，并注册SensitiveServer
	s := grpc.NewServer()
	proto.RegisterKeyWordsMatchServer(s, &server.KwServer{})

	// 启动服务
	err = s.Serve(lis)
	if err != nil {
		log.Fatal(err)
	}
}
