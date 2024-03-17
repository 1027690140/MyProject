// Code generated by protoc-gen-go-grpc. DO NOT EDIT.

package proto

import (
	context "context"
	grpc "google.golang.org/grpc"
	codes "google.golang.org/grpc/codes"
	status "google.golang.org/grpc/status"
)

// This is a compile-time assertion to ensure that this generated file
// is compatible with the grpc package it is being compiled against.
const _ = grpc.SupportPackageIsVersion6

// KeyWordsMatchClient is the client API for KeyWordsMatch service.
//
// For semantics around ctx use and closing/ending streaming RPCs, please refer to https://pkg.go.dev/google.golang.org/grpc/?tab=doc#ClientConn.NewStream.
type KeyWordsMatchClient interface {
	Match(ctx context.Context, in *MatchRequest, opts ...grpc.CallOption) (*MatchResponse, error)
}

type keyWordsMatchClient struct {
	cc grpc.ClientConnInterface
}

func NewKeyWordsMatchClient(cc grpc.ClientConnInterface) KeyWordsMatchClient {
	return &keyWordsMatchClient{cc}
}

func (c *keyWordsMatchClient) Match(ctx context.Context, in *MatchRequest, opts ...grpc.CallOption) (*MatchResponse, error) {
	out := new(MatchResponse)
	err := c.cc.Invoke(ctx, "/sensitive.KeyWordsMatch/Match", in, out, opts...)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// KeyWordsMatchServer is the server API for KeyWordsMatch service.
// All implementations must embed UnimplementedKeyWordsMatchServer
// for forward compatibility
type KeyWordsMatchServer interface {
	Match(context.Context, *MatchRequest) (*MatchResponse, error)
	mustEmbedUnimplementedKeyWordsMatchServer()
}

// UnimplementedKeyWordsMatchServer must be embedded to have forward compatible implementations.
type UnimplementedKeyWordsMatchServer struct {
}

func (*UnimplementedKeyWordsMatchServer) Match(context.Context, *MatchRequest) (*MatchResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "method Match not implemented")
}
func (*UnimplementedKeyWordsMatchServer) mustEmbedUnimplementedKeyWordsMatchServer() {}

func RegisterKeyWordsMatchServer(s *grpc.Server, srv KeyWordsMatchServer) {
	s.RegisterService(&_KeyWordsMatch_serviceDesc, srv)
}

func _KeyWordsMatch_Match_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(MatchRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	if interceptor == nil {
		return srv.(KeyWordsMatchServer).Match(ctx, in)
	}
	info := &grpc.UnaryServerInfo{
		Server:     srv,
		FullMethod: "/sensitive.KeyWordsMatch/Match",
	}
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return srv.(KeyWordsMatchServer).Match(ctx, req.(*MatchRequest))
	}
	return interceptor(ctx, in, info, handler)
}

var _KeyWordsMatch_serviceDesc = grpc.ServiceDesc{
	ServiceName: "sensitive.KeyWordsMatch",
	HandlerType: (*KeyWordsMatchServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "Match",
			Handler:    _KeyWordsMatch_Match_Handler,
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "keywords.proto",
}
