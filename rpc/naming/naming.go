package naming

import "context"

type Instance struct {
	Env      string            `json:"env"`
	AppID    string            `json:"AppID"`
	Hostname string            `json:"hostname"`
	Addrs    []string          `json:"addrs"`
	Version  string            `json:"version"`
	Zone     string            `json:"zone"`
	Region   string            `json:"region"`
	Labels   []string          `json:"labels"`
	Metadata map[string]string `json:"metadata"`
	Status   uint32            `json:"status"`
}

type Registry interface {
	Register(context.Context, *Instance) (context.CancelFunc, error)
	Fetch(context.Context, string) ([]*Instance, bool)
	Close() error
}
