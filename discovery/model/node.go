package model

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"

	//	"github.com/gin-gonic/gin"
	"log"

	"service_discovery/configs"
	"service_discovery/pkg/errcode"
	"service_discovery/pkg/httputil"
	"service_discovery/pkg/queue"
	uid "service_discovery/pkg/uniqueId"

	"strconv"
	"time"
)

// AP
// cluster

// status
const (
	// InstanceStatusUP Ready to receive traffic
	InstanceStatusUP = uint32(1)
	// InstancestatusWating Intentionally shutdown for traffic
	InstancestatusWating = uint32(1) << 1
)

// node is a special client
type Node struct {
	config      *Config
	addr        string
	status      int
	registerURL string
	cancelURL   string
	renewURL    string
	pollURL     string
	pollsURL    string
	zone        string
}

func NewNode(config *configs.GlobalConfig, addr string, zonenumber string) *Node {
	return &Node{
		addr:        addr,
		status:      configs.NodeStatusDown, //default set down
		registerURL: fmt.Sprintf("http://%s%s", addr, configs.RegisterURL),
		cancelURL:   fmt.Sprintf("http://%s%s", addr, configs.CancelURL),
		renewURL:    fmt.Sprintf("http://%s%s", addr, configs.RenewURL),
		zone:        zonenumber,
	}
}

//TODO
//Synchronization failure: record the failure queue, and resend failed requests to quickly repair

func (node *Node) Register(instance *Instance) error {
	return node.call(context.Background(), node.registerURL, configs.Register, instance, nil)
}

func (node *Node) Cancel(instance *Instance) error {
	return node.call(context.Background(), node.cancelURL, configs.Cancel, instance, nil)
}

func (node *Node) Renew(instance *Instance) error {
	var res *Instance
	err := node.call(context.Background(), node.renewURL, configs.Renew, instance, &res)
	if err == errcode.ServerError {
		log.Printf("node call %s ! renew error %s \n", node.renewURL, err)
		node.status = configs.NodeStatusDown //node down
		return err
	}
	if err == errcode.NotFound { //register
		log.Printf("node call %s ! renew not found, register again \n", node.renewURL)
		return node.call(context.Background(), node.registerURL, configs.Register, instance, nil)
	}
	if err == errcode.Conflict && res != nil {
		return node.call(context.Background(), node.registerURL, configs.Register, res, nil)
	}
	return err
}

func (node *Node) call(ctx context.Context, uri string, action configs.Action, instance *Instance, data interface{}) error {
	params := make(map[string]interface{})
	params["env"] = instance.Env
	params["AppID"] = instance.AppID
	params["hostname"] = instance.Hostname
	params["replication"] = true //broadcast stop here
	switch action {
	case configs.Register:
		params["addrs"] = instance.Addrs
		params["status"] = instance.Status
		params["version"] = instance.Version
		params["reg_timestamp"] = strconv.FormatInt(instance.RegTimestamp, 10)
		params["dirty_timestamp"] = strconv.FormatInt(instance.DirtyTimestamp, 10)
		params["latest_timestamp"] = strconv.FormatInt(instance.LatestTimestamp, 10)
	case configs.Renew:
		params["dirty_timestamp"] = strconv.FormatInt(instance.DirtyTimestamp, 10)
		params["renew_timestamp"] = time.Now().UnixNano()
	case configs.Cancel:
		params["latest_timestamp"] = strconv.FormatInt(instance.LatestTimestamp, 10)

	}
	// 生产全局ID
	id := uid.GenerateSnowflakeID()

	//失败参数
	ASyncPamars := NewPamars(id, ctx, uri, action, instance, data, params)

	//request other server
	resp, err := httputil.HttpPost(uri, params)
	if err != nil {
		log.Println(err, "retry HttpPost")
		go AsyncRetry(ctx, uri, action, instance, data, ASyncPamars)
		return <-ASyncPamars.failBackFail
	}
	res := Response{}
	err = json.Unmarshal([]byte(resp), &res)
	if err != nil {
		log.Println(err, "retry Unmarshal")

		go AsyncRetry(ctx, uri, action, instance, data, ASyncPamars)
		return <-ASyncPamars.failBackFail

	}
	if res.Code != configs.StatusOK { //code!=200
		log.Printf("uri is (%v),response code (%v)\n", uri, res.Code)
		json.Unmarshal([]byte(res.Data), data)
		go AsyncRetry(ctx, uri, action, instance, data, ASyncPamars)
		<-ASyncPamars.failBackFail
		return errcode.Conflict
	}

	return nil
}

// 同步失败 异步处理
func AsyncRetry(ctx context.Context, uri string, action configs.Action, instance *Instance, data interface{}, failPamars *ASyncPamars) error {

	// 加入失败队列
	if atomic.LoadInt32(&gobalfailList.size) == 0 { //队列为空
		q := queue.NewQueue()
		q.Add(failPamars)
		gobalfailList.list.Add(q)
		atomic.AddInt32(&gobalfailList.size, 1)
	} else {
		p := gobalfailList.list.Peek().(*queue.Queue)
		p.Add(failPamars)
	}
	//异步执行
	go AsyncFailBcak(gobalfailList)

	//等待执行结果
	select {

	//异步执行成功
	case <-failPamars.failBackSucess:
		idd := <-failPamars.failBackId
		if failPamars.id == idd { //确保id对应
			return nil
		}
		log.Printf("--Failback -- servicePath --- task ID mismatchr ")
		return errors.New("task ID mismatch")

	//异步执行失败
	case err := <-failPamars.failBackFail:
		log.Printf("--Failback -- servicePath ---again fail ")
		return err
	//异步执行超时
	case <-ctx.Done():
		// 设置超时 逻辑删除，队列中的任务不执行  Set timeout tombstone, task  do not execute
		failPamars.isTimeOut = true
		failPamars.failBackFail <- fmt.Errorf("---time over ")
		return ctx.Err()
	}
}
