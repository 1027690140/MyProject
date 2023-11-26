package model

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"service_discovery/configs"
	"service_discovery/errors"
	"service_discovery/pkg/errcode"
	"service_discovery/pkg/httputil"
	"sync/atomic"
	"time"
)

type Discovery struct {
	config *configs.GlobalConfig
	client *httputil.Client

	lastHost  string
	protected bool
	Registry  *Registry
	Nodes     atomic.Value
}

// discovery
func NewDiscovery(config *configs.GlobalConfig, connOption *ConnOption) *Discovery {
	//init discovery
	dis := &Discovery{
		protected: false,
		config:    config,
		Registry:  NewRegistry(connOption), //init registry
	}
	//new nodes from config file
	dis.Nodes.Store(NewNodes(config))

	//sync data from other nodes
	dis.initSync()

	//将自己注册到注册中心 ,实现自发现
	instance := dis.regSelf()
	//定期续约，每 30s 发送一次续约请求
	go dis.renewTask(instance)

	//节点感知 维护节点列表
	go dis.nodesPerception()
	//exit protected mode
	go dis.exitProtect()
	return dis
}

// 遍历所有节点，获取注册表数据，依次注册到本地。这里注意只有当所有数据同步完毕后，该注册中心才可对外提供服务，切换为上线状态。
func (dis *Discovery) initSync() {
	nodes := dis.Nodes.Load().(*Nodes)
	for _, node := range nodes.AllNodes() {
		if node.Addr == nodes.SelfAddr {
			continue
		}
		uri := fmt.Sprintf("http://%s%s", node.Addr, configs.FetchAllURL)
		resp, err := httputil.HttpPost(uri, nil)
		if err != nil {
			fmt.Println(err)
			continue
		}
		var res struct {
			Code    int                    `json:"code"`
			Message string                 `json:"message"`
			Data    map[string][]*Instance `json:"data"`
		}
		err = json.Unmarshal([]byte(resp), &res)
		if err != nil {
			fmt.Printf("get from %v error : %v", uri, err)
			continue
		}
		if res.Code != configs.StatusOK {
			fmt.Printf("get from %v error : %v", uri, res.Message)
			continue
		}
		dis.protected = false
		for _, v := range res.Data {
			for _, instance := range v {
				dis.Registry.Register(instance, instance.LatestTimestamp)
			}
		}
	}
	nodes.SetUp()
}

// register current discovery node
func (dis *Discovery) regSelf() *Instance {
	fmt.Println("### discovery node register self when start ###")
	//register
	now := time.Now().UnixNano()
	instance := &Instance{
		Env:             dis.config.Env,
		Hostname:        dis.config.Hostname,
		AppID:           configs.DiscoveryAppID,
		Addrs:           []string{"http://" + dis.config.HttpServer},
		Status:          configs.NodeStatusUp,
		RegTimestamp:    now,
		UpTimestamp:     now,
		LatestTimestamp: now,
		RenewTimestamp:  now,
		DirtyTimestamp:  now,
	}
	dis.Registry.Register(instance, now)
	//注册后同步到其他集群
	dis.Nodes.Load().(*Nodes).Replicate(configs.Register, instance) //broadcast
	return instance
}

// renew current discovery node
func (dis *Discovery) renewTask(instance *Instance) {
	now := time.Now().UnixNano()
	ticker := time.NewTicker(configs.RenewInterval) //30 second
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fmt.Println("### discovery node renew every 30s ###")
			_, err := dis.Registry.Renew(instance.Env, instance.AppID, instance.Hostname)
			if err == errcode.NotFound {
				dis.Registry.Register(instance, now)
				dis.Nodes.Load().(*Nodes).Replicate(configs.Register, instance)
			} else {
				dis.Nodes.Load().(*Nodes).Replicate(configs.Renew, instance)
			}
		}
	}
}

func (dis *Discovery) CancelSelf() {
	fmt.Println("### discovery node cancel self when exit ###")
	dis.Registry.Cancel(dis.config.Env, configs.DiscoveryAppID, dis.config.Hostname, time.Now().UnixNano())
	instance := &Instance{
		Env:      dis.config.Env,
		Hostname: dis.config.Hostname,
		AppID:    configs.DiscoveryAppID,
	}
	dis.Nodes.Load().(*Nodes).Replicate(configs.Cancel, instance) //broadcast
}

// 定时更新 discovery nodes list ；  被动通知更新（TODO）
func (dis *Discovery) nodesPerception() {
	var lastTimestamp int64
	ticker := time.NewTicker(configs.NodePerceptionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			fmt.Println("### discovery node protect tick ###")
			fmt.Printf("### discovery nodes,len (%v) ###\n", len(dis.Nodes.Load().(*Nodes).AllNodes()))
			fetchData, err := dis.Registry.Fetch(dis.config.Zone, dis.config.Env, configs.DiscoveryAppID, configs.NodeStatusUp, lastTimestamp)
			if err != nil || fetchData == nil {
				continue
			}
			var nodes []string
			for _, instance := range fetchData.Instances {
				for _, addr := range instance.Addrs {
					u, err := url.Parse(addr)
					if err == nil {
						nodes = append(nodes, u.Host)
					}
				}
			}
			lastTimestamp = fetchData.LatestTimestamp

			//config update new nodes
			config := new(configs.GlobalConfig)
			*config = *dis.config
			config.Nodes = nodes

			ns := NewNodes(config)
			ns.SetUp()
			dis.Nodes.Store(ns)
			fmt.Printf("### discovery protect change nodes,len (%v) ###\n", len(dis.Nodes.Load().(*Nodes).AllNodes()))
		}
	}
}

// discovery exit protect after 1 minute
func (dis *Discovery) exitProtect() {
	time.Sleep(configs.ProtectTimeInterval)
	dis.protected = false
	fmt.Println("### discovery node exit protect after 60s ###")
}

// FetchAll fetch all instances of all the department.
func (d *Discovery) FetchAll(c context.Context) (im map[string][]*Instance) {
	return d.Registry.FetchAll()
}

// Fetchs fetch multi app by appids.
func (d *Discovery) Fetchs(c context.Context, arg *ArgFetchs) (is map[string]*FetchData, err error) {
	is = make(map[string]*FetchData, len(arg.AppID))
	for _, appid := range arg.AppID {
		i, err := d.Registry.Fetch(arg.Zone, arg.Env, appid, arg.Status, 0)
		if err != nil {
			fmt.Errorf("Fetchs fetch appid(%v) err", err)
			continue
		}
		is[appid] = i
	}
	return
}

// Polls hangs request and then write instances when that has changes, or return NotModified.
func (d *Discovery) Polls(c context.Context, arg *ArgPolls, connOption *ConnOption) (ch chan map[string]*InstanceInfo, new bool, err error) {
	return d.Registry.Polls(c, arg, connOption)
}

// DelConns delete conn of host in appid
func (d *Discovery) DelConns(arg *ArgPolls) {
	d.Registry.DelConns(arg)
}

// Set set metadata,color,status of instance.
func (d *Discovery) Set(c context.Context, arg *ArgSet) (err error) {
	if err := d.Registry.Set(c, arg); err != nil {
		err = errors.ParamsErr
	}
	return
}
