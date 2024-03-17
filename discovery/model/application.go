package model

import (
	"encoding/json"
	"fmt"
	"log"
	"service_discovery/pkg/errcode"
	"sync"
	"time"
)

// Apps app distinguished by zone\region   存一个zone\region里面的Application
type Apps struct {
	apps            map[string]*Application
	lock            sync.RWMutex
	latestTimestamp int64
}

// NewApps return new Apps.
func NewApps() *Apps {
	return &Apps{
		apps: make(map[string]*Application),
	}
}

// 应用级
type Application struct {
	AppID           string
	instances       map[string]*Instance
	latestTimestamp int64
	lock            sync.RWMutex
}

func NewApplication(AppID string) *Application {
	return &Application{
		AppID:     AppID,
		instances: make(map[string]*Instance),
	}
}

// 注册app add instance
// 返回 *Instance 实例信息
// 返回 bool true 已有实例升级 false 新增实例
func (app *Application) AddInstance(in *Instance, latestTimestamp int64) (*Instance, bool) {
	app.lock.Lock()
	defer app.lock.Unlock()
	appIns, ok := app.instances[in.Hostname]
	if ok { //exist
		in.UpTimestamp = appIns.UpTimestamp
		//dirtytimestamp
		if in.DirtyTimestamp < appIns.DirtyTimestamp {
			log.Println("register exist dirty timestamp")
			in = appIns
		}
	}
	//add or update instances
	app.instances[in.Hostname] = in
	app.upLatestTimestamp(latestTimestamp)
	returnIns := new(Instance)
	*returnIns = *in
	return returnIns, !ok
}

// Renew 续约
func (app *Application) Renew(hostname string) (*Instance, bool) {
	app.lock.Lock()
	defer app.lock.Unlock()
	appIn, ok := app.instances[hostname]
	if !ok {
		return nil, ok
	}
	//modify renew time
	appIn.RenewTimestamp = time.Now().UnixNano()
	//get copy
	return copyInstance(appIn), true
}

// Cancel 取消
func (app *Application) Cancel(hostname string, latestTimestamp int64) (*Instance, bool, int) {
	newInstance := new(Instance)
	app.lock.Lock()
	defer app.lock.Unlock()
	appIn, ok := app.instances[hostname]
	if !ok {
		return nil, ok, 0
	}
	//delete hostname
	delete(app.instances, hostname)
	appIn.LatestTimestamp = latestTimestamp
	app.upLatestTimestamp(latestTimestamp)
	*newInstance = *appIn
	return newInstance, true, len(app.instances)
}

// 获取所有*Instance
func (app *Application) GetAllInstances() []*Instance {
	app.lock.RLock()
	defer app.lock.RUnlock()
	rs := make([]*Instance, 0, len(app.instances))
	for _, instance := range app.instances {
		newInstance := new(Instance)
		*newInstance = *instance
		rs = append(rs, newInstance)
	}
	return rs
}

// 获取*Instance信息
// status=1 return up 实例
func (app *Application) GetInstance(status uint32, latestTime int64) (*FetchData, *errcode.Error) {
	app.lock.RLock()
	defer app.lock.RUnlock()
	if latestTime >= app.latestTimestamp { //not modify
		return nil, errcode.NotModified
	}
	fetchData := FetchData{
		Instances:       make([]*Instance, 0),
		LatestTimestamp: app.latestTimestamp,
	}
	var exists bool
	for _, instance := range app.instances {
		if status&instance.Status > 0 {
			exists = true
			newInstance := copyInstance(instance)
			fetchData.Instances = append(fetchData.Instances, newInstance)
		}
	}
	if !exists {
		return nil, errcode.NotFound
	}
	return &fetchData, nil
}

func (app *Application) GetInstanceLen() int {
	app.lock.RLock()
	instanceLen := len(app.instances)
	app.lock.RUnlock()
	return instanceLen
}

// update app latest_timestamp
func (app *Application) upLatestTimestamp(latestTimestamp int64) {
	if latestTimestamp <= app.latestTimestamp { //already latest
		latestTimestamp = app.latestTimestamp + 1 //increase
	}
	app.latestTimestamp = latestTimestamp
}

// deep copy
func copyInstance(src *Instance) *Instance {
	dst := new(Instance)
	*dst = *src
	//copy addrs
	dst.Addrs = make([]string, len(src.Addrs))
	for i, addr := range src.Addrs {
		dst.Addrs[i] = addr
	}
	return dst
}

// App get app by zone.
func (p *Apps) App(zone string) (as []*Application) {
	p.lock.RLock()
	if zone != "" {
		a, ok := p.apps[zone]
		if !ok {
			p.lock.RUnlock()
			return
		}
		as = []*Application{a}
	} else {
		for _, a := range p.apps {
			as = append(as, a)
		}
	}
	p.lock.RUnlock()
	return
}

func appsKey(appid, env string) string {
	return fmt.Sprintf("%s-%s", appid, env)
}

func (r *Registry) app(appid, env, zone string) (as []*Application, a *Apps, ok bool) {
	key := appsKey(appid, env)
	r.lock.RLock()
	a, ok = r.appm[key]
	r.lock.RUnlock()
	if ok {
		as = a.App(zone)
	}
	return
}

// Set set new status,metadata,color  of instance .
func (a *Application) Set(changes *ArgSet) (ok bool) {
	a.lock.Lock()
	defer a.lock.Unlock()
	var (
		dst     *Instance
		setTime int64
	)
	if changes.SetTimestamp == 0 {
		setTime = time.Now().UnixNano()
	}
	for i, hostname := range changes.Hostname {
		if dst, ok = a.instances[hostname]; !ok {
			fmt.Errorf("SetWeight hostname(%s) not found", hostname)
			return
		}
		if len(changes.Status) != 0 {
			if changes.Status[i] != InstanceStatusUP && changes.Status[i] != InstancestatusWating {
				fmt.Errorf("SetWeight change status(%d) is error", changes.Status[i])
				ok = false
				return
			}
			dst.Status = changes.Status[i]
			if dst.Status == InstanceStatusUP {
				dst.UpTimestamp = setTime
			}
		}
		if len(changes.Metadata) != 0 {
			if err := json.Unmarshal([]byte(changes.Metadata[i]), &dst.Metadata); err != nil {
				fmt.Errorf("set change metadata err %s", changes.Metadata[i])
				ok = false
				return
			}
		}
		dst.LatestTimestamp = setTime
		dst.DirtyTimestamp = setTime
	}
	a.updateLatest(setTime)
	return
}

// UpdateLatest update LatestTimestamp.
func (p *Apps) UpdateLatest(latestTime int64) {
	p.lock.Lock()
	if latestTime <= p.latestTimestamp {
		// insure increase
		latestTime = p.latestTimestamp + 1
	}
	p.latestTimestamp = latestTime
	p.lock.Unlock()

}

func (a *Application) updateLatest(latestTime int64) {
	if latestTime <= a.latestTimestamp {
		// insure increase
		latestTime = a.latestTimestamp + 1
	}
	a.latestTimestamp = latestTime
}
