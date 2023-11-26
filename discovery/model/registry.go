package model

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"service_discovery/configs"
	"service_discovery/errors"
	"service_discovery/pkg/errcode"
	"sync"
	"time"
)

// 连接池参数选择
type ConnOption struct {
	DisableKeepAlives   bool          //是否开启长连接 用于轮询等等
	MaxIdleConns        int           //最大连接数
	MaxIdleConnsPerHost int           //每个host可以发出的最大连接个数
	Timeout             time.Duration //超时时间
	IdleConnTimeout     time.Duration //多长时间未使用自动关闭连接
}

type Registry struct {
	appm map[string]*Apps        //key=AppID+env    -> apps
	apps map[string]*Application //key=AppID+env

	conns     map[string]map[string]*conn // 连接池 region.zone.env.AppID-> host
	connQueue *ConnQueue
	lock      sync.RWMutex
	cLock     sync.RWMutex
	scheduler *scheduler

	gd *Guard //protect

}

func NewRegistry(connOption *ConnOption) (r *Registry) {
	r = &Registry{
		apps:  make(map[string]*Application),
		appm:  make(map[string]*Apps),
		gd:    new(Guard),
		conns: make(map[string]map[string]*conn),
		connQueue: &ConnQueue{
			headPos: 0,
			head:    make([]*conn, 1),
			tail:    make([]*conn, 1),
		},
		scheduler: newScheduler(r),
	}

	go r.evictTask()
	return r
}

// register
func (r *Registry) Register(instance *Instance, latestTimestamp int64) (*Application, *errcode.Error) {
	key := getKey(instance.AppID, instance.Env)
	r.lock.RLock()
	app, ok := r.apps[key]
	r.lock.RUnlock()
	if !ok { //new app
		app = NewApplication(instance.AppID)
	}

	//add instance
	_, isNew := app.AddInstance(instance, latestTimestamp)
	if isNew {
		r.gd.incrNeed()
	}
	//add into registry apps
	r.lock.Lock()
	r.apps[key] = app
	r.lock.Unlock()
	return app, nil
}

// Renew 客户端上报，设 TTL
func (r *Registry) Renew(env, AppID, hostname string) (*Instance, *errcode.Error) {
	//find app
	app, ok := r.getApplication(AppID, env)
	if !ok {
		return nil, errcode.NotFound
	}
	//modify instance renewtime
	in, ok := app.Renew(hostname)
	if !ok {
		return nil, errcode.NotFound
	}
	r.gd.incrCount()
	return in, nil
}

// cancel
func (r *Registry) Cancel(env, AppID, hostname string, latestTimestamp int64) (*Instance, *errcode.Error) {
	//find app
	app, ok := r.getApplication(AppID, env)
	if !ok {
		return nil, errcode.NotFound
	}
	instance, ok, insLen := app.Cancel(hostname, latestTimestamp)
	if !ok {
		return nil, errcode.NotFound
	}
	//if instances is empty, delete app from apps
	if insLen == 0 {
		r.lock.Lock()
		delete(r.apps, getKey(AppID, env))
		r.lock.Unlock()
	}
	r.gd.decrNeed()
	return instance, nil
}

// get by appname
func (r *Registry) Fetch(zone, env, AppID string, status uint32, latestTime int64) (*FetchData, *errcode.Error) {
	app, ok := r.getApplication(AppID, env)
	if !ok {
		return nil, errcode.NotFound
	}
	return app.GetInstance(status, latestTime) //err = not modify
}

// get all key=AppID, value=[]*Instance
func (r *Registry) FetchAll() map[string][]*Instance {
	apps := r.getAllApplications()
	rs := make(map[string][]*Instance)
	for _, app := range apps {
		rs[app.AppID] = append(rs[app.AppID], app.GetAllInstances()...)
	}
	return rs
}

func (r *Registry) getApplication(AppID, env string) (*Application, bool) {
	key := getKey(AppID, env)
	r.lock.RLock()
	app, ok := r.apps[key]
	r.lock.RUnlock()
	return app, ok
}

func (r *Registry) getAllApplications() []*Application {
	r.lock.RLock()
	defer r.lock.RUnlock()
	apps := make([]*Application, 0, len(r.apps))
	for _, app := range r.apps {
		apps = append(apps, app)
	}
	return apps
}

func (r *Registry) evictTask() {
	ticker := time.Tick(configs.CheckEvictInterval)
	resetTicker := time.Tick(configs.ResetGuardNeedCountInterval)
	for {
		select {
		case <-ticker:
			log.Println("### registry evict task every 60s ###")
			r.gd.storeLastCount()
			r.evict()
		case <-resetTicker:
			//那么续约时间超过阈值（默认90 秒）忽略不会剔除。但如果续约时间超过最大阈值（默认3600 秒），那么不管是否开启保护都要剔除。
			log.Println("### registry reset task every 15min ###")
			var count int64
			for _, app := range r.getAllApplications() {
				count += int64(app.GetInstanceLen())
			}
			r.gd.setNeed(count)
		}
	}
}

// 删除过期实例
func (r *Registry) evict() {
	now := time.Now().UnixNano()
	var expiredInstances []*Instance
	apps := r.getAllApplications()
	var registryLen int
	protectStatus := r.gd.selfProtectStatus()
	for _, app := range apps {
		registryLen += app.GetInstanceLen()
		allInstances := app.GetAllInstances()
		for _, instance := range allInstances {
			delta := now - instance.RenewTimestamp
			//实例过期时间超过阈值，且不在保护期内，或者超过最大阈值，那么剔除
			if !protectStatus && delta > int64(configs.InstanceExpireDuration) ||
				delta > int64(configs.InstanceMaxExpireDuration) {
				expiredInstances = append(expiredInstances, instance)
			}
		}
	}
	//考虑到GC因素，设置一个上限evictionLimit进行驱逐。
	//驱逐到期时，使用“Knuth-Shuffle”算法实现随机驱逐
	evictionLimit := registryLen - int(float64(registryLen)*configs.SelfProtectThreshold)
	expiredLen := len(expiredInstances)
	if expiredLen > evictionLimit {
		expiredLen = evictionLimit
	}
	if expiredLen == 0 {
		return
	}
	for i := 0; i < expiredLen; i++ {
		j := i + rand.Intn(len(expiredInstances)-i)
		expiredInstances[i], expiredInstances[j] = expiredInstances[j], expiredInstances[i]
		expiredInstance := expiredInstances[i]
		r.Cancel(expiredInstance.Env, expiredInstance.AppID, expiredInstance.Hostname, now)
		//取消广播
		// global.Discovery.Nodes.Load().(*Nodes).Replicate(configs.Cancel, expiredInstance)
		log.Printf("### evict instance (%v, %v,%v)###\n", expiredInstance.Env, expiredInstance.AppID, expiredInstance.Hostname)
	}
}

func getKey(AppID, env string) string {
	return fmt.Sprintf("%s-%s", AppID, env)
}

// Fetch fetch all instances by AppID for Polls.
func (r *Registry) FetchInstanceInfo(zone, env, AppID string, latestTime int64, status uint32) (info *InstanceInfo, err error) {
	key := getKey(AppID, env)
	r.lock.RLock()
	a, ok := r.appm[key]
	r.lock.RUnlock()
	if !ok {
		err = errors.NothingFound
		return
	}
	info, err = a.InstanceInfo(zone, latestTime, status)
	if err != nil {
		return
	}
	sch := r.scheduler.Get(AppID, env)
	if sch != nil {
		info.Scheduler = sch.Zones
	}
	return
}

// 轮询挂起请求，然后在有更改时写入实例，或者返回 NotModified。
func (r *Registry) Polls(c context.Context, arg *ArgPolls, connOption *ConnOption) (ch chan map[string]*InstanceInfo, new bool, err error) {
	var (
		ins = make(map[string]*InstanceInfo, len(arg.AppID))
		in  *InstanceInfo
	)
	if len(arg.AppID) != len(arg.LatestTimestamp) {
		arg.LatestTimestamp = make([]int64, len(arg.AppID))
	}
	for i := range arg.AppID {
		in, err = r.FetchInstanceInfo(arg.Zone, arg.Env, arg.AppID[i], arg.LatestTimestamp[i], InstanceStatusUP)
		if err == errors.NothingFound {
			fmt.Errorf("Polls zone(%s) env(%s) AppID(%s) error(%v)", arg.Zone, arg.Env, arg.AppID[i], err)
			return
		}
		if err == nil {
			ins[arg.AppID[i]] = in
			new = true
		}
	}
	if new {
		ch = make(chan map[string]*InstanceInfo, 1)
		ch <- ins
		return
	}
	// err = errors.NotModified  如果没有变更则timeout秒后返回304
	r.cLock.Lock()
	for i := range arg.AppID {
		k := pollKey(arg.Env, arg.AppID[i])
		if _, ok := r.conns[k]; !ok {
			r.conns[k] = make(map[string]*conn, 1)
		}
		connection, ok := r.conns[k][arg.Hostname]
		if !ok {
			if ch == nil {
				ch = make(chan map[string]*InstanceInfo, 5) // 注意：同一hostname上可能有多个连接！！！
			}
			connection = newConn(ch, arg.LatestTimestamp[i], arg, connOption)
			log.Println("Polls from(%s) new connection(%d)", arg.Hostname, connection.count)
		} else {
			connection.count++ // 注意：同一hostname上可能有多个连接！！！
			// 超过数量限制
			if connection.count > connOption.MaxIdleConns {
				r.connQueue.pushBack(connection)

				over := make(chan struct{})
				// connection 入队后，启动一个 goroutine 监听 connection.wait，在后台更新连接。
				go func(connection *conn) {
					for {
						if r.connQueue.headPos != 0 {
							r.connQueue.popFront()
						}
						select {
						// 执行popFront()时将从队列中删除连接，wait<- struct{}{}，从而 goroutine 退出。
						case <-connection.wait:
							over <- struct{}{}
							return
						case <-c.Done():
							fmt.Errorf("conn ctx time out")
							return
						case <-time.After(connection.connOption.Timeout):
							fmt.Errorf("conn time out")
							return
						}
					}
				}(connection)
				//等待出队
				select {
				case <-over:
				}
				// 主要是保证对应的连接数量不超过限制
			}

			if ch == nil {
				ch = connection.ch
			}
			log.Println("Polls from(%s) reuse connection(%d)", arg.Hostname, connection.count)
		}
		r.conns[k][arg.Hostname] = connection
	}
	r.cLock.Unlock()
	return
}

func pollKey(env, appid string) string {
	return fmt.Sprintf("%s.%s", env, appid)
}

// DelConns delete conn of host in appid
func (r *Registry) DelConns(arg *ArgPolls) {
	r.cLock.Lock()
	for i := range arg.AppID {
		k := pollKey(arg.Env, arg.AppID[i])
		conns, ok := r.conns[k]
		if !ok {
			fmt.Errorf("DelConn key(%s) not found", k)
			continue
		}
		if connection, ok := conns[arg.Hostname]; ok {
			if connection.count > 1 {
				fmt.Println("DelConns from(%s) count decr(%d)", arg.Hostname, connection.count)
				connection.count--
			} else {
				fmt.Println("DelConns from(%s) delete(%d)", arg.Hostname, connection.count)
				delete(conns, arg.Hostname)
			}
		}
	}
	r.cLock.Unlock()
}

// Set set metadata,color,status of instance.
func (r *Registry) Set(c context.Context, arg *ArgSet) (err error) {
	var ok bool
	a, _, _ := r.app(arg.AppID, arg.Env, arg.Zone)
	if len(a) == 0 {
		return
	}
	if ok = a[0].Set(arg); !ok {
		return
	}
	r.Broadcast(arg.Env, arg.AppID)
	if !ok {
		err = errors.ParamsErr
	}
	return
}

func (r *Registry) Broadcast(env, appid string) {
	key := pollKey(env, appid)
	r.cLock.Lock()
	defer r.cLock.Unlock()
	conns, ok := r.conns[key]
	if !ok {
		return
	}
	delete(r.conns, key)
	for _, conn := range conns {
		ii, _ := r.Fetch(conn.arg.Zone, env, appid, InstanceStatusUP, 0) // TODO(felix): latesttime!=0 increase

		for i := 0; i < conn.count; i++ {
			select {
			case conn.instanch <- ii: // NOTE: if chan is full, means no poller.
				log.Println("Broadcast to(%s) success(%d)", conn.arg.Hostname, i+1)
			case <-time.After(time.Millisecond * 500):
				log.Println("Broadcast to(%s) failed(%d) maybe chan full", conn.arg.Hostname, i+1)
			}
		}
	}
}
