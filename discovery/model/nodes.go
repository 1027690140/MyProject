package model

import (
	//"github.com/gin-gonic/gin"

	"log"
	"service_discovery/configs"
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
	log.Printf("################ %v #################\n", len(nodes.nodes))
	if len(nodes.nodes) == 0 {
		return nil
	}
	for _, node := range nodes.nodes {
		if node.addr != nodes.selfAddr {
			log.Printf("### replicate node(%v),action(%v),hostname(%v) ###\n", node.addr, action, instance.Hostname)
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
