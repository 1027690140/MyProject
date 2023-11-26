package trace

import (
	"context"
)

type invoker func(ctx context.Context, interceptors []interceptor2, h handler) error
type handler func(ctx context.Context)
type interceptor2 func(ctx context.Context, h handler, ivk invoker) error

// 串联所有 interceptor
func getInvoker(ctx context.Context, interceptors []interceptor2, cur int, ivk invoker) invoker {
	if cur == len(interceptors)-1 {
		return ivk
	}
	return func(ctx context.Context, interceptors []interceptor2, h handler) error {
		return interceptors[cur+1](ctx, h, getInvoker(ctx, interceptors, cur+1, ivk))
	}
}

// 返回第一个 interceptor 作为入口
func getChainInterceptor(ctx context.Context, interceptors []interceptor2, ivk invoker) interceptor2 {
	if len(interceptors) == 0 {
		return nil
	}
	if len(interceptors) == 1 {
		return interceptors[0]
	}
	return func(ctx context.Context, h handler, ivk invoker) error {
		return interceptors[0](ctx, h, getInvoker(ctx, interceptors, 0, ivk))
	}
}
