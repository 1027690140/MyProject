package naming

import (
	"context"
	"testing"
)

func TestNewDiscovery(t *testing.T) {
	nodes := []string{"localhost:8881"}
	conf := &Config{Nodes: nodes, Env: "dev"}
	dis := New(conf)
	t.Log(dis)
	select {}
}

func TestRegister(t *testing.T) {
	nodes := []string{"localhost:8881"}
	conf := &Config{Nodes: nodes, Env: "dev"}
	dis := New(conf)

	instance := &Instance{
		AppId:    "aabb",
		Addrs:    []string{"localhost:55", "localhost:66"},
		Hostname: "ccdd",
	}
	ctx := context.Background()
	dis.Register(ctx, instance)
	select {}
}
