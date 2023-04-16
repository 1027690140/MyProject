package model

import (
	//"github.com/gin-gonic/gin"

	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"service_discovery/configs"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Zone info.

//go:generate easytags $GOFILE json
type Zone struct {
	Src string         `json:"src"`
	Dst map[string]int `json:"dst"`
}

type Nodes struct {
	nodes    []*Node
	selfAddr string
	Zone     string
	zones    map[string][]*Node
}

// new nodes
func NewNodes(c *configs.GlobalConfig) *Nodes {
	nodes := make([]*Node, 0, len(c.Nodes))
	for _, addr := range c.Nodes {
		n := NewNode(c, addr, c.Zone)
		nodes = append(nodes, n)
	}
	return &Nodes{
		nodes:    nodes,
		selfAddr: c.HttpServer,
	}
}

// replicate to other nodes
func (nodes *Nodes) Replicate(action configs.Action, instance *Instance) error {
	fmt.Printf("################ %v #################\n", len(nodes.nodes))
	if len(nodes.nodes) == 0 {
		return nil
	}
	for _, node := range nodes.nodes {
		if node.addr != nodes.selfAddr {
			fmt.Printf("### replicate node(%v),action(%v),hostname(%v) ###\n", node.addr, action, instance.Hostname)
			go nodes.action(node, action, instance)
		}
	}
	return nil
}

// Myself returns whether or not myself.
func (ns *Nodes) Myself(addr string) bool {
	return ns.selfAddr == addr
}

// UP marks status of myself node up.
func (ns *Nodes) UP() {
	for _, nd := range ns.nodes {
		if ns.Myself(nd.addr) {
			nd.status = configs.NodeStatusUp
		}
	}
}

// use node action
func (nodes *Nodes) action(node *Node, action configs.Action, instance *Instance) {
	switch action {
	case configs.Register:
		go node.Register(instance)
	case configs.Renew:
		go node.Renew(instance)
	case configs.Cancel:
		go node.Cancel(instance)
	}
}

// get all nodes
func (nodes *Nodes) AllNodes() []*Node {
	nodeRs := make([]*Node, 0, len(nodes.nodes))
	for _, node := range nodes.nodes {
		n := &Node{
			addr:   node.addr,
			status: node.status,
		}
		nodeRs = append(nodeRs, n)
	}
	return nodeRs
}

// set up current node
func (nodes *Nodes) SetUp() {
	for _, node := range nodes.nodes {
		if node.addr == nodes.selfAddr {
			node.status = configs.NodeStatusUp
		}
	}
}

var nodeIdx uint64

func (d *Discovery) pickNode() string {
	nodes, ok := d.Nodes.Load().([]string)
	if !ok || len(nodes) == 0 {
		return d.config.Nodes[rand.Intn(len(d.config.Nodes))]
	}
	return nodes[atomic.LoadUint64(&nodeIdx)%uint64(len(nodes))]
}

func (d *Discovery) switchNode() {
	atomic.AddUint64(&nodeIdx, 1)
}

// 通过nodes[idx] 发起polls discovery的请求，实时监听discovery nodes节点变更。如果收到poll接口变更推送则进行，否则进行⑤
// 收到节点变更推送，对比收到的节点列表与本地旧的列表是否一致 ，否则重新用新的列表打散获取新的nodes
// 如果polls接口返回err 不为nil则转到idx+1，否则如果code=-304 说明服务节点无变更，则重新继续polls
// 收到err不为nil,说明当前节点可能故障，将idx+1 并且last_timestamp设置为0进行节点切换重新发起polls
// 收到code=-304 说明服务节点无变更，则重新继续polls
func (d *Discovery) polls(ctx context.Context) (apps map[string]*InstanceInfo, err error) {
	var (
		lastTss []int64
		appIDs  []string
		host    = d.pickNode()
		changed bool
	)
	if host != d.lastHost {
		d.lastHost = host
		changed = true
	}
	var mu sync.RWMutex
	mu.RLock()
	c := d.config
	for k, v := range d.Registry.apps {
		if changed {
			v.latestTimestamp = 0
		}
		appIDs = append(appIDs, k)
		lastTss = append(lastTss, v.latestTimestamp)
	}
	mu.RUnlock()
	if len(appIDs) == 0 {
		return
	}
	uri := fmt.Sprintf("/api/polls", host)
	res := new(struct {
		Code int                      `json:"code"`
		Data map[string]*InstanceInfo `json:"data"`
	})
	params := url.Values{}
	params.Set("env", c.Env)
	params.Set("hostname", c.Hostname)
	for _, appid := range appIDs {
		params.Add("appid", appid)
	}
	for _, ts := range lastTss {
		params.Add("latest_timestamp", strconv.FormatInt(ts, 10))
	}
	if err = d.client.Get(ctx, uri, "", params, res); err != nil {
		d.switchNode()
		fmt.Errorf("discovery: client.Get(%s) error(%+v)", uri+"?"+params.Encode(), err)
		return
	}

	info, _ := json.Marshal(res.Data)
	for _, app := range res.Data {
		if app.LatestTimestamp == 0 {
			fmt.Errorf("discovery: client.Get(%s) latest_timestamp is 0,instances:(%s)", uri+"?"+params.Encode(), info)
			return
		}
	}
	apps = res.Data
	return
}

// copy
var randSeed = rand.New(rand.NewSource(time.Now().UnixNano()))

func shuffle(n int, swap func(i, j int)) {
	if n < 0 {
		panic("invalid argument to Shuffle")
	}
	i := n - 1
	for ; i > 1<<31-1-1; i-- {
		j := int(randSeed.Int63n(int64(i + 1)))
		swap(i, j)
	}
	for ; i > 0; i-- {
		j := int(randSeed.Int31n(int32(i + 1)))
		swap(i, j)
	}
}

func (d *Discovery) newSelf(zones map[string][]*Instance) {
	ins, ok := zones[d.config.Zone]
	if !ok {
		return
	}
	var nodes []string
	for _, in := range ins {
		for _, addr := range in.Addrs {
			u, err := url.Parse(addr)
			if err == nil && u.Scheme == "http" {
				nodes = append(nodes, u.Host)
			}
		}
	}
	// diff old nodes
	var olds int
	for _, n := range nodes {
		if node, ok := d.Nodes.Load().([]string); ok {
			for _, o := range node {
				if o == n {
					olds++
					break
				}
			}
		}
	}
	if len(nodes) == olds {
		return
	}
	// FIXME: we should use rand.Shuffle() in golang 1.10
	shuffle(len(nodes), func(i, j int) {
		nodes[i], nodes[j] = nodes[j], nodes[i]
	})
	d.Nodes.Store(nodes)
}
