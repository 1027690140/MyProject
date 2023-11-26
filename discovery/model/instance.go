package model

import (
	"service_discovery/errors"
	"time"
)

// 服务实例 Instance
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
	// timestamp
	RegTimestamp    int64 `json:"reg_timestamp"`
	UpTimestamp     int64 `json:"up_timestamp"`
	RenewTimestamp  int64 `json:"renew_timestamp"`
	DirtyTimestamp  int64 `json:"dirty_timestamp"`
	LatestTimestamp int64 `json:"latest_timestamp"`
}

// InstanceInfo the info get by consumer , use for http polls
type InstanceInfo struct {
	Instances       map[string][]*Instance `json:"instances"`
	Scheduler       []Zone                 `json:"scheduler,omitempty"`
	LatestTimestamp int64                  `json:"latest_timestamp"`
}

func NewInstance(req *ArgRequest) *Instance {
	now := time.Now().UnixNano()
	instance := &Instance{
		Env:             req.Env,
		AppID:           req.AppID,
		Hostname:        req.Hostname,
		Addrs:           req.Addrs,
		Version:         req.Version,
		Status:          req.Status,
		RegTimestamp:    now,
		UpTimestamp:     now,
		RenewTimestamp:  now,
		DirtyTimestamp:  now,
		LatestTimestamp: now,
	}
	return instance
}

// Instances return slice of instances.
func (a *Application) Instances() (is []*Instance) {
	a.lock.RLock()
	is = make([]*Instance, 0, len(a.instances))
	for _, i := range a.instances {
		ni := new(Instance)
		*ni = *i
		is = append(is, ni)
	}
	a.lock.RUnlock()
	return
}

// InstanceInfo 返回实例切片。如果 up 为真，则返回所有状态实例，否则返回 up 状态实例
func (p *Apps) InstanceInfo(zone string, latestTime int64, status uint32) (ci *InstanceInfo, err error) {
	p.lock.RLock()
	defer p.lock.RUnlock()
	if latestTime >= p.latestTimestamp {
		err = errors.NotModified
		return
	}
	ci = &InstanceInfo{
		LatestTimestamp: p.latestTimestamp,
		Instances:       make(map[string][]*Instance),
	}
	var ok bool
	for z, app := range p.apps {
		if zone == "" || z == zone {
			ok = true
			instance := make([]*Instance, 0)
			for _, i := range app.Instances() {
				// 如果 up 为false返回所有状态实例
				if i.filter(status) {
					// if i.Status == InstanceStatusUP && i.LatestTimestamp > latestTime
					ni := new(Instance)
					*ni = *i
					instance = append(instance, ni)
				}
			}
			ci.Instances[z] = instance
		}
	}
	if !ok {
		err = errors.NothingFound
	} else if len(ci.Instances) == 0 {
		err = errors.NotModified
	}
	return
}

func (i *Instance) filter(status uint32) bool {
	return status&i.Status > 0
}
