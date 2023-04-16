
# RPC：
实现服务注册发现能力、异步调用、插件化扩展、负载均衡、熔断、限流、负载均衡功能
主要基于TCP协议进行通信，以大端字节序传输
### 协议和编解码 
支持 Gob 和 JSON    

RPC 消息格式编码设计如下，协议消息头定义定长 6 字节（byte），依次放置魔术数（用于校验），协议版本，消息类型（区分请求/响应），压缩类型，序列化协议类型，消息 ID  每个占 1 个字节（8 个 bit）。可扩展追加  元数据 等信息用于做服务治理。
![image.png](https://cdn.nlark.com/yuque/0/2023/png/29261914/1676516101926-d4a0b5f0-599e-474c-9c04-d387bcc580df.png#averageHue=%23edebe8&clientId=uf2d882b6-95ff-4&from=paste&height=140&id=u9efabf65&name=image.png&originHeight=175&originWidth=1075&originalType=binary&ratio=1.25&rotation=0&showTitle=false&size=17967&status=done&style=none&taskId=ufd8b8c70-1c61-4143-b72c-853dd822f83&title=&width=860)
```go
type Header [HEADER_LEN]byte

type RPCMsg struct {
	*Header
	Error         error
    ServiceAppID  string            // 服务应用ID
	ServiceClass  string
	ServiceMethod string
	Payload       []byte
}
```
类名、方法名、payload 均为不定长部分，定义对应的长度字段标识不定长的长度，SPLIT_LEN 代表各部分长度，是 int32 类型，相当于 4 个 byte，所以 `SPLIT_LEN 为 4`。
也就是每一段消息中间有一段 `[start : start + 4 ]`  存当前消息体长度，防止粘包
如：Decode时 
```go
	//service method len
	start = end
	end = start + SPLIT_LEN
	methodLen := binary.BigEndian.Uint32(data[start:end]) //start,start+4

	//service method
	start = end
	end = start + int(methodLen)
	msg.ServiceMethod = util.ByteToString(data[start:end]) //start+4, start+4+len
```
大致流程：
消息读取后反解析，按发送顺序依次还原 Header、类名、方法名、Payload，不定长部分都有对应的长度保存，因此可以顺利解析到所有数据。
主要分为两个方法：`**Send** `方法将消息编码成二进制格式并发送到 **io.Writer**，`**Decode** `方法从 **io.Reader** 读取二进制数据并解析成 RPC 消息。

1. `**Send** `方法首先将消息头部`**Header**`写入到 **io.Writer** 中，然后
   1. 计算消息体总长度，写入到 `io.Writer` 中。依次记录各部分长度和内容
   2. 接下来依次写入服务类别长度`len(msg.ServiceAppID)`、服务类别名称`ServiceMethod`......
   3. 服务应用ID`len(msg.ServiceAppID)`、服务方法名称`ServiceAppID`
   4. `len(msg.ServiceClass)`、服务方法名称ServiceClass
   5. 服务方法长度`len(msg.ServiceMethod)`、服务方法名称`ServiceMethod`
   6. 负载数据长度`len(msg.Payload)`。最后将负载数据`msg.Payload`写入到 **io.Writer** 中。
2. `**Decode** `方法首先从 **io.Reader** 中读取并验证消息头部的`magicNumber`判断是否合法。接下来读取总消息体长度，然后读取全部消息体数据。并将所有的消息体数据读入一个字节数组**data**中。接着根据消息体数据的格式，依次从**data**数组中取出`**msg.ServiceAppID**`、`**msg.ServiceMethod**`**、**`**msg.**ServiceClass`和`**msg.Payload**`的内容，并赋值给对应的字段。。

### 服务端
服务端注册服务，并监听请求
```go
// TCP Server
type RPCServer struct {
    engine       *Engine           
    listener     Listener           // Used to bind the service instance.
    registry     naming.Registry    // 
    cancelFunc   context.CancelFunc // 
    ServerOption ServerOption       //
    Plugins      PluginContainer    //
}


type Listener interface {
	Run() error
	SetHandler(string, Handler)
	SetPlugins(PluginContainer)
	Close()
	GetAddrs() []string
	Shutdown()
}
```

执行顺序：
```go
NewRPCServer 			  ->  	
RPCServer.Listener.Run()  ->		// Run
RPCServer.register()	  -> 		// register
go Listener.acceptConn()  -> 		// 建立连接
go Listener.handleConn(conn) ->  	// 处理连接
    Listener.receiveData(conn)   ->   	// Decode
    Listener.Handlers[msg.ServiceClass].handle(method , params ) -> // 执行对应方法
    Listener.sendData(conn, Res)  		// 返回结果
```
```go
1.//read from network
msg, err := l.receiveData(conn)

2.//decode
coder := global.Codecs[msg.Header.SerializeType()] //get SerializeType
err = coder.Decode(msg.Payload, &inArgs) 

3.//call local service
handler, ok := l.Handlers[msg.ServiceClass]
l.Plugins.BeforeCall(msg.ServiceClass, msg.ServiceMethod, inArgs) 
result, err := handler.Handle(msg.ServiceMethod, inArgs)
l.Plugins.AfterCall(msg.ServiceClass, msg.ServiceMethod, inArgs, result, err)//Plugins 

4.//encode
encodeRes, err := coder.Encode(result) 

5.//send result
l.Plugins.BeforeWrite(encodeRes)  //Plugins 
err = l.sendData(conn, encodeRes)     
l.Plugins.AfterWrite(encodeRes, err) //Plugins 
```
![image.png](https://cdn.nlark.com/yuque/0/2023/png/29261914/1681297892580-4746306a-7a1c-4c9a-9708-c9105eaba648.png#averageHue=%2336504e&clientId=uc5c9d5eb-ef81-4&from=paste&height=32&id=u0aadb7b1&name=image.png&originHeight=40&originWidth=314&originalType=binary&ratio=1.25&rotation=0&showTitle=false&size=2045&status=done&style=none&taskId=u4f8d7276-fde5-4f15-bc6c-ad470667da8&title=&width=251.2)
改进：因为是服务启动初始化时进行所有服务注册，但是考虑动态注册、并发 Handlers可以用 Sync.Map 
服务端与注册中心的交互包括服务启动时会将自身服务信息（监听地址和端口）写入注册中心，开启定时续约，在服务关闭退出时会注销自身的注册信息。

**服务启动注册**
服务注册数据包括运行环境（env），服务标识（appId），主机名（hostname），服务地址（addrs）等。向服务注册中心发起注册请求，失败后会进行重试，如果重试失败将会终止退出并关闭服务。 
```go
	//在服务端入口，实例化 RPCServer 时传入注册中心依赖。
    //服务注册中心
    conf := &Registry.Config{Nodes: config.RegistryAddrs, Env: config.Env}
    discovery := Registry.New(conf)
    //注入依赖
    srv := NewRPCServer(option, discovery)
```
服务启动后会自动开启服务续约保持服务状态。 
**服务退出注销**
服务关闭退出时需要将其从注册中心一并移除，和启动顺序相反，启动时先将服务启动再去暴露给注册中心，而退出时先从注册中心注销，再去关闭服务。
协程间状态同步通过 `context.WithCancel` 的方式，将服务注销方法提供给外层协程调用。当执行 Close() 时，会执行 `cancelFun()`，进而 `cancel()` 触发 `ctx.Done()`，完成 `dis.cancel() `，将服务从注册中心注销。
```go
        cancelFunc := context.CancelFunc(func() {
            cancel()
            <-ch
        })
        for {
            select {
            case <-ctx.Done():
                discovery.cancel(instance) //服务注销
                ch <- struct{}{}
            }
        }
```
服务关闭时，除了不再接受新请求外，还需要考虑处理中的请求，不能因为服务关闭而强制中断所有处理中的请求。根据请求所处阶段不同，可以分别设置“逻辑关闭”，告知服务调用方当前服务处于关闭流程，不再接受请求了。
根据请求所处阶段不同，设置关闭记号：

1. 服务端接收到客户端连接阶段。如果此时发现服务关闭，不再往下执行，直接返回。
2. 开始处理请求阶段。优先判断服务是否正在关闭，关闭则退出处理流程。通过设置一个全局标志位（shutdown），关闭服务时原子操作设置其值为 1，并通过判断值是否为 1，来去拦截请求。
3. 请求已进入服务实际处理阶段。因为已经是处理中，将请求处理完成。统计处理中的请求数量，并确保这些请求全部执行完成，然后安全退出。

```go
func (svr *RPCServer) Shutdown() {
	//从服务注册中心注销 
    if svr.cancelFunc != nil {
        svr.cancelFunc()
    }
    //关闭当前服务
    if svr.listener != nil {
        svr.listener.Shutdown()
    }
}
```
### 客户端
客户端调用对应的方法
```go
type RPCClientProxy struct {
	ClientOption      ClientOption
	failMode          FailMode
	registry          Registry   // 绑定服务注册中心实例
	mutex             sync.RWMutex
	loadBalance       LoadBalance
	servers           []string

	client     Client

	requestGroup singleflight.Group
}
```
例：Service：`UserService.user.GetUser`
```go
type Service struct {
	Class  string
	Method string
    AppId  string   
    Addrs  []string // 一个服务实例可能部署在不同机器
}
```

代理客户端初始化时根据appId，向服务注册中心获取该标识对应的实例列表。先从本地缓存中获取，如不存在则从远程注册中心拉取并缓存下来。
```go
NewClientProxy  ->
ClientProxy.discoveryService()  ->
ClientProxy.Call()  ->
	NewService()  	->
	Connect()   ->
	ClientProxy.Clietn.Invoke(ctx, service, stub, params...) ->
```
```go
Clietn.Invoke(ctx , service , methodPtr  , params ) ->  // 发起 RPC 调用
Clietn.MakeFunc(service, methodPtr )  -> 				// 通过反射机制生成代理函数
Clietn.wrapCall(ctx, methodPtr , params...)   ->  	    // 传参，执行函数
```
`MakeFunc()`函数中** **其中，入参 `service `是一个结构体，包含了 RPC 调用的类名和方法名和appid；入参 `methodPtr `是一个函数指针，代表要进行 RPC 调用的函数。`MakeFunc `函数利用 `reflect `包获取 `methodPtr `函数的元素，即函数本身，并根据 **cli.ClientOption** 的设置进行数据编解码和网络传输操作以及负载均衡操作。
`wrapCall()`调用 `reflect.ValueOf(stub).Elem().Call(in)`函数通过反射调用函数，并传入参数 **in**，最终获取函数执行结果并返回。
`**reflect.ValueOf(methodPtr).Elem().Set(reflect.MakeFunc(container.Type(), handler))**`
**在**`handler`函数中，执行发送请求，解码，读取请求操作
 ![image.png](https://cdn.nlark.com/yuque/0/2023/png/29261914/1681385491129-9fb711c5-4554-4551-bd67-4c31074eadae.png#averageHue=%232d4744&clientId=u953c7ccb-69a0-4&from=paste&height=127&id=u00471358&name=image.png&originHeight=209&originWidth=414&originalType=binary&ratio=1.25&rotation=0&showTitle=false&size=11098&status=done&style=none&taskId=u5b08261b-1ca1-465a-a3c8-48c24f69ca5&title=&width=251.20001220703125)

### 失败策略  
客户端调用实现失败策略

- `Failover` 失败转移		故障转移，换个服务端实例地址再试
- `Failsafe` 失败安全		应用场景，可以用于写入审计日志等操作。
- `Failfast` 快速失败		直接返回报错
- `Failretry` 失败重试     		重试retries 次
- `Failback` 失败自动恢复，如果调用失败，记录日志，将这次调用加入内存中的失败队列中，异步重试。失败队列由二维队列组成，对于这个队列中的失败调用，会在另一个线程中进行异步重试，重试如果再发生失败，则会忽略，即使重试调用成功，原来的调用方也感知不到了。（适合对于实时性要求不高，且不需要返回值的一些异步操作。 ）
### 熔断
**熔断器状态**

- 关闭状态，默认关闭的，不做任何拦截，对统计窗口做检查变更。检测到失败到了的阈值，会开启熔断。
- 半开状态，开启状态，如果过了冷却时间，会进入半开状态尝试调用。允许少量的服务请求，如果调用都成功（或一定比例）则认为恢复了，关闭熔断器，否则认为还没好，又回到熔断器打开状态。
- 打开状态，不做处理，此时对下游的调用在内部直接返回错误，不发出请求，但是在一定的时间周期以后，会进入下一个半熔断状态。

**根据当前熔断器状态：**

- 熔断器关闭，窗口时间滚动一个时间窗口期windowInterval ，时间窗口期也是 breaker 初始化时设置，计数统计发生在同一窗口期
- 熔断器打开，过了冷却期状态转移为半开，会进入新的计数窗口期，窗口期开始时间增加冷却期休眠时间 sleepTimeout
- 半开状态，不做窗口期处理

**熔断策略**
分别记录窗口的批次，窗口开始的时间，窗口期内所有请求数，所有成功数，所有失败数，连续成功数，连续失败数。
在初始化熔断器和熔断器状态变更的时候都会新开统计窗口

- 根据错误计数，如果一个时间窗口期内失败数 >= n 次，开启熔断。
- 根据连续错误计数，一个时间窗口期内连续失败 >=n 次，开启熔断。
- 根据错误比例，一个时间窗口期内错误占比 >= n%  &&  错误计数>= 阈值，开启熔断，设置阈值(防止第一次请求失败（错误比1） 。

半开-————> 关闭 ：连续成功数 >= breaker.halfMaxCalls  或  错误比例 < 阈值 
关闭-————> 半开： 冷却时间
**熔断过程**

- **执行前      **根据熔断状态进行判断
- **执行后	**执行成功与否改变熔断相关参数
### 限流
基于滑动窗口的计数器限流算法
局限：窗口算法对于流量限制是定速的，对细粒度时间控制突发流量控制能力就有限了。
令牌桶算法
公式：可用令牌数=（当前请求时间-上次请求时间）*令牌生成速率 + 上次使用后剩余令牌数
通过redis Hash 。

1. **num**：当前桶中令牌的数量
2. **lasttime**：上次向桶中添加令牌的时间戳

其中 **num** 和 **lasttime** 都是字符串类型。每次需要获取令牌时，先从 Hash 中取出 **num** 和 **lasttime** 字段，然后根据当前时间戳和上次添加令牌的时间戳计算增量，最后根据算出的增量更新 **num** 字段和 **lasttime** 字段的值，判断是否可以获取令牌。
### 负载均衡
客户端实现负载均衡 ：
随机算法 、 轮询算法
加权轮询算法：某节点被选中的次数≈(本节点权重/全部权重) * 总分配次数。

1. 遍历所有节点，计算出所有节点的总权重 totalWeight；
2. 对于每个节点，都将其当前权重加上其原始权重
3. 选出当前权重最大的节点作为 bestNode；
4. 减掉选出的节点的当前权重 totalWeight（即 bestNode.currentWeight -= totalWeight）；
5. 返回选出的节点的地址 bestNode。

一致性hash算法 ：
```go
type HashBalancer struct {
	hash         Hash              // 产生uint32类型的函数
	circle       Circle            // 环
	virtualNodes int               // 虚拟节点个数
	virtualMap   map[uint32]string // 点到主机的映射
	members      map[string]bool   // 主机列表
	sync.RWMutex
}
```
```go
AddByKey(key string)添加一个新的服务器节点，它加锁，以避免并发修改导致的问题。如果该节点已经存在，则直接返回，否则将该节点添加到主机列表中，并根据虚拟节点数为该节点生成多个虚拟节点，计算出每个虚拟节点在哈希环上的位置，同时将该虚拟节点与主机映射添加到virtualMap中。最后，更新哈希环并释放锁。

Remove(elt string)移除一个服务器节点，它首先加锁，以避免并发修改导致的问题。如果该节点不存在，则直接返回，否则从主机列表中删除该节点，并循环删除该节点的所有虚拟节点，同时从virtualMap中删除与这些虚拟节点相关的映射。最后，更新哈希环并释放锁。

ForceSet(keys ...string)强制设置主机列表，先获取当前哈希环中的所有服务器节点，并循环检查是否需要将其移除。然后，循环传递的所有键，并检查它是否已经存在于当前哈希环中，如果不存在，则将其添加到主机列表中。最后，更新哈希环。

GetByKey(key string) 根据传递的键获取服务器节点。获取该键在哈希环上的位置。最后，从虚拟映射中获取该服务器节点并返回。

eltKey(key string, idx int) 生成一个虚拟节点映射键值

search(key uint32)   在哈希环中二分查找 最优节点 （在哈希环上，节点是按照它们的哈希值排序的。）
```
通过工厂模式封装负载均衡策略。初始化代理客户端时，选择负载均衡算法，通过负载均衡器选取出服务端实例节点地址，并发起连接
### TCP连接池
实现TCP连接池，在短连接频繁的情况下，可以减小GC，提高效率
实现扩容、缩容、保活等机制
1、连接池的基本属性
连接池属性包含**最大连接数、最小连接数、当前连接数、活跃连接、空闲连接**等，定义分别如下：

- 最大连接数：连接池最多保持的连接数，如果客户端请求超过次数，便要根据池满的处理机制来处理没有得到连接的请求
- 最小连接数：连接池初始化或是其他时间，连接池内一定要存储的连接数
- 当前连接数：
- 空闲连接数：连接池一直保持的空闲连接数，无论这些连接被使用与否都会被保持。如果客户端对连接池的使用量不大，便会造成服务端连接资源的浪费
- 活跃连接 ： 最近使用过的连接 `time.now()> p.idleTimeout+poolconn.lastusedtime`  active就变成idle   连接池pools扩容时，优先扩容活跃连接对应的pool
- 空闲连接： 比较久没有使用过的 连接池pools缩容时，优先缩容空闲连接对应的pool

2、动态扩容、缩容机制

- 扩容：当请求到来时，如果连接池中没有空闲的连接，同时连接数也没有达到最大活跃连接数，便会按照特定的增长策略创建新的连接服务该请求，同时用完之后归还到池中，而非关闭连接，同时对连接池扩容
- 缩容：当连接池一段时间没有被使用，同时池中的连接数超过了最大空闲连接数，那么便会关闭一部分连接，使池中的连接数始终维持在最大空闲连接数
- 记录 expand 和 shrink 的时间 ，防止频繁扩容缩容
- 单个addr对应的pool 缩容 根据 p.lastused[]对conn使用时间进行排序，优先回收最久没有使用的conn
```go
            type ConnectionPool struct {
                .......
            	lastused    []time.Time      // 连接池最后使用时间，用于connections[]缩容
                connections []chan *poolConn // 连接池 放poolConn
                .......
            }


			// 计算需要关闭的连接数量
			numConnsToClose := p.currConns - p.shrinkNum
			// 对连接池中的所有连接按最后使用时间从新到旧排序()
			sort.Slice(p.connections, func(i, j int) bool {
				return p.lastused[j].Before(p.lastused[i])
			})

			// 关闭连接池中最旧的 numConnsToClose 个连接
```

- 全局连接池，定期开启空闲连接检查，**双向list + map  实现 LRU 算法**
```plsql
type LRU struct {
	lock sync.RWMutex // 锁

	activeList   *list.List               // 活跃链表
	inactiveList *list.List               // 不活跃链表
	addrMap      map[string]*list.Element // 地址到节点元素的映射
	lastUsedTime map[string]time.Time     // 记录地址最后使用时间的映射
	maxEntries   int                      // 最大元素个数
	idleTimeout  time.Duration            // 超时时间
	lruEvitChan  chan struct{}            //更新lru
	Pools        *Pools
}
```
使用两个链表。类似于linux的active_list 活跃链表 ，inactive_list 不活跃链表。 
加入节点先加入inactive_list ，而inactive_list 访问过的节点移动到active_list 
开启定时异步任务 使得 active_list 中 使用时间 `LRU.lastusedtime[addr] - time.now() > p.idleTimeout` 就移到 `inactive_list` 
`inactive_list` 中 使用时间 `LRU.lastusedtime[addr] - time.now()> p.idleTimeout` 就缩容
```go
// RunIdleCheck 用于定时检查不活跃链表中的元素是否超时，如果超时则通知缩容
func (lru *LRU) RunIdleCheck(idleTimeout time.Duration) {
    ..........
	for {
		select {
		case <-lru.lruEvitChan:
		case <-ticker.C:
            element := lru.activeList.Back()
			// 遍历 activeList，将超时未使用的节点移到 inactiveList
			for element != nil {
				if current.Sub(lastUsedTime) > idleTimeout {
					.....
					// 将节点移到 inactiveList
					lru.inactiveList.PushFront(addr)
					.....
				} else {
					// 继续遍历 activeList
				}
			}

			element = lru.inactiveList.Back()
			// 遍历 inactiveList，将超时未使用的节点通知缩容并删除节点
			for element != nil {
				.....
				if current.Sub(lastUsedTime) > idleTimeout {
					// 缩容
					go lru.Pools.connPools[addr].shrink()
					.....
					// 删除当前节点并获取下一个节点,防止重复缩容
					lru.inactiveList.Remove(element)
					.....
				} else {
					// 继续遍历 inactiveList
				}
			}

		case <-lru.Pools.poolClosedChan:
			return
		}
	}
}
```
3、实现单个连接池keepalive保活机制，后台用一个线程或者协程定期的执行保活函数，进行连接的保活，缺点是感知连接的失效会有一定的延迟，从池中仍然有可能获取到失效的连接。
4、连接池满的处理机制
当连接池容量超上限时，有2种处理机制：

   1. 对于连接池新建的连接，并返回给客户端，当客户端用完时，如果池满则关闭连接，否则放入池中
   2. 设置一定的超时时间来等待空闲连接。需要客户端加入重试机制，避免因超时之后获取不到空闲连接产生的错误

5、连接池异常的容错机制
连接池异常时，退化为新建连接的方式，避免影响正常请求，同时，需要相关告警通知开发人员
6、开启异步连接池回收
对于空闲连接（超过允许最大空闲时间）超时后，主动关闭连接池中的连接
对于空闲连接数量过多，异步回收
7、异步重试
删除、新增、放回连接失败，异步处理
8、实现多地址连接池，统一管理
![](https://raw.githubusercontent.com/pandaychen/pandaychen.github.io/master/blog_img/redis/connection-pool-basic-flow.png#from=url&id=WWAgp&originHeight=691&originWidth=1129&originalType=binary&ratio=1.25&rotation=0&showTitle=false&status=done&style=none&title=)
```go
type poolChecker struct {
	indexFreq         time.Duration // 获取index超时时间
	connectionTimeout time.Duration // 连接超时时间
	idleTimeout       time.Duration // 空闲连接超时时间
	idleCheckFreq     time.Duration // 空闲连接检查频率
	keepAliveInterval time.Duration // 保活检查时间
	keepAliveStopChan chan struct{} //关闭keepalive
	keepAliveChan     chan struct{} //通知keepalive
}

// 锁
type poolLocker struct {
	lock          sync.RWMutex // 锁
	expandingLock sync.Mutex   // 扩容锁
	shrinkingLock sync.Mutex   // 缩容锁
	idlecheckLock sync.RWMutex // idlecheckLock锁
	keepaliveLock sync.RWMutex // keepalive检查锁
}
// 记录 expander 和 shrinker 的时间 ，防止频繁扩容缩容
type timeRecorder struct {
	expandReocrd      time.Time
	shrinkRecord      time.Time
	minShrinkInterval time.Duration // 最小缩容间隔
	minExpandInterval time.Duration // 最小扩容间隔
}

// ConnectionPool 某个addr的连接池
type ConnectionPool struct {
	poolLocker // 锁
	poolChecker
	timeRecorder
	adrr        string
	newConnFunc func(string) (net.Conn, error)

	connections []chan *poolConn // 连接池 放poolConn
	lastused    []time.Time      // 连接池最后使用时间，用于connections[]缩容

	reuse bool // // 如果 reuse 为真且池处于 MaxActive 限制，则 Get() 重用 要返回的连接，如果 reuse 为 false 并且池处于 MaxActive 限制， 创建一个一次性连接返回。

	roundRobinCounter int32 // index

	minConns    int // 最小连接数
	maxConns    int // 最大连接数
	maxIdle     int // 最大空闲连接数
	currConns   int // 当前连接数 = 被取出的+还在池子里的
	poolNum     int // connections大小
	shrinkNum   int // 缩容数目
	connChanNum int //  []chan *poolConn 中每个chan大小

	timerPool *sync.Pool // 池化保存 Timer

	closed     bool          // 是否关闭
	poolClosed chan struct{} // 通知关闭
}
```

```go
// chan to stop
type poolStoper struct {
	checkIdleStopChan chan struct{} //关闭checkIdle
	retryNewStopChan  chan struct{} //关闭retry
	retryDelStopChan  chan struct{} //关闭retry
	retryPutStopChan  chan struct{} //关闭retry
	poolClosedChan    chan struct{} //通知关闭
}

// 全局pool 管理不同addr 的pool  对所有的空闲、活跃连接进行管理
type Pools struct {
	poolStoper      // chan to stop
	LRU        *LRU // lru
	// 下面的连接数 指的是addr种类数 类似set
	curActiveConnsNum int //当前活跃连接数
	curIdleConnsNum   int //当前空闲连接数  ： 即使用频率低于activeThreshold的连接
	maxActiveConnsNum int //最大空闲连接数
	maxIdleConnsNum   int //最大活跃连接数

	connPools map[string]*ConnectionPool //不同addr对应的连接池

	activeThreshold int // 活跃阈值 超过该值就是活跃的连接

	connNum        map[string]int // 连接  统计所以连接以及对应的连接数
	activeConnsNum map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	idleConnsNum   map[string]int // 活跃连接 统计活跃连接以及对应的连接数
	lastUsedTime   map[string]time.Time
	idleCheckFreq  time.Duration // 空闲连接检查频率

	idlecheckLock sync.RWMutex // 空闲连接检查锁
	closeLock     sync.RWMutex // 关闭锁

	retryNewList chan *PoolOptions    // 重试新建队列
	retryDelList chan *ConnectionPool // 重试删除队列
	retryPutList chan []interface{}   // 重试put队列

	closed bool // 是否关闭
}
```


使用例子：
```go
	
	p, _ := pool.New(newConnFunc, &DefaultOptions)
	defer p.ClosePool()
	conn, err := p.Get()
	defer p.Put(conn)

	// 全局连接池
	p,_ := NewPools( &PoolsParameter , opts)
	defer p.Close()
	conn, err := p.GetConn(adrr)
	defer p.PutConn(conn)
    _:p.NewPoolConnection(PoolOptions)	
	_,p.ClosePool(adrr)
```


idlecheck
### 协程池
实现一个简易的协程池
同时监控 Pool 的状态 , 定时检查过期的 worker 并关闭
```go
// Pool 1.维护一个worker队列 2.提供Submit方法提交任务 3.提供Close方法关闭Pool
type Pool struct {
	// Pool的容量，即最大worker数量
	capacity int32
	// Pool中当前活跃的worker数量
	active int32
	// 过期时间，即worker最大空闲时间，超过该时间则worker会被关闭
	expiredDuration time.Duration

	workers []*Worker
	//通知关闭
	release chan stop

	lock sync.RWMutex
	// 保证只关闭一次
	once sync.Once
}

type Worker struct {
	pool *Pool

	task chan taskFunc

	lastUsed time.Time
}
```
还没实现带参协程池
### 插件
首先定义了一个 **PluginContainer** 接口和一些 **Plugin** 接口，这些接口定义了不同的插件功能。然后定义了一个 **pluginContainer** 结构体来存储所有的插件
在运行过程中，通过调用 **Add** 方法将需要的插件添加到 **pluginContainer** 中，然后通过调用不同的 **PluginContainer** 接口方法触发对应的插件实现，从而实现不同的功能扩展。比如，通过调用 **BeforeReadHook** 方法触发所有的 **BeforeReadPlugin** 插件实现预处理读操作，通过调用 **AfterCallHook** 方法触发所有的 **AfterCallPlugin** 插件实现处理完函数调用后的处理。这些插件实现可以在实现时进行自定义和定制，比如添加日志、鉴权、性能监控等功能。
```go
type PluginContainer interface {
    Add(plugin Plugin)
    Remove(plugin Plugin)
    All() []Plugin

    Register(string, interface{}) error
    Unregister(string) error
    ConnAccept(net.Conn) (net.Conn, bool)
    
    BeforeRead() error
    AfterRead(*protocol.RPCMsg, error) error
    BeforeCall(string, string, []interface{}) error
    AfterCall(string, string, []interface{}, []interface{}, error) error
    BeforeWrite([]byte) error
    AfterWrite([]byte, error) error
}

type Plugin interface{}
```
例：

1. **Register(name string, class interface{}) error**：将一个名字为 name 的钩子注册到所有已经加载的插件中，类的实例为 class。
2. **Unregister(name string) error**：从所有已经加载的插件中注销名字为 name 的钩子。
3. **ConnAccept(conn net.Conn) (net.Conn, bool)**：执行所有插件中的 ConnAcceptPlugin 接口的 HandleConnAccept 方法，传入参数 conn，返回两个值：一个连接对象和一个布尔值，表示是否接受该连接。
4. **BeforeRead() error**：执行所有插件中的 BeforeReadPlugin 接口的 BeforeRead 方法，用于在每次读取客户端数据前进行处理。

其中，插件需要实现对应的接口：

1. **RegisterPlugin**：实现 **Register(name string, class interface{}) error** 方法，用于在插件初始化时注册钩子函数。
2. **ConnAcceptPlugin**：实现 **HandleConnAccept(conn net.Conn) (net.Conn, bool)** 方法，用于在处理连接前对连接进行处理。
3. **BeforeReadPlugin**：实现 **BeforeRead() error** 方法，在每次读取客户端数据前进行处理。

在func (l *RPCListener) Run()   func (l *RPCListener) handleConn(conn net.Conn)  等方法中调用插件
**执行 ConnAccept ** **BeforeCall ** **BeforeReadPlugin  等插件中的方法**
```go
go l.acceptConn()

func (l *RPCListener) acceptConn() {
	for {
		conn, err := l.nl.Accept()
		if err != nil {
			select { //done
			case <-l.getDoneChan():
				log.Println("server closed done")
				return
			default:
			}

			if e, ok := err.(net.Error); ok && e.Temporary() { //网络发生临时错误,不退出重试
				log.Printf("server accept network error: %v", err)
				time.Sleep(5 * time.Millisecond)
				continue
			}

			log.Printf("server accept err: %v\n", err)
			return
		}

		//plugin aop
		conn, ok := l.Plugins.ConnAccept(conn)
		if !ok {
			//如果 ConnAcceptHook 插件处理 conn 的过程中返回了 false，说明连接不被接受。因此，执行 conn.Close() 关闭该连接，继续执行下一次循环，处理下一个连接
			conn.Close()
			continue
		}
		log.Printf("server accepted conn: %v\n", conn.RemoteAddr().String())

		//create new routine worker each connection
		go l.handleConn(conn)
	}
}
```

```go
// handle each connection
func (l *RPCListener) handleConn(conn net.Conn) {
	//关闭挡板
	if l.isShutdown() {
		return
	}

	//catch panic
	defer func() {
		if err := recover(); err != nil {
			log.Printf("server %s catch panic err:%s\n", conn.RemoteAddr(), err)
		}
		l.CloseConn(conn)
	}()

	for {
		//关闭挡板
		if l.isShutdown() {
			return
		}

		//readtimeout
		startTime := time.Now()
		if l.ServerOption.ReadTimeout != 0 {
			conn.SetReadDeadline(startTime.Add(l.ServerOption.ReadTimeout))
		}

		//处理中任务数+1
		atomic.AddInt32(&l.handlingNum, 1)
		//任意退出都会导致处理中任务数-1
		defer atomic.AddInt32(&l.handlingNum, -1)

		//read 
		msg, err := l.receiveData(conn)
		if err != nil || msg == nil {
			log.Println("server receive error:", err) //超时
			return
		}

		//decode
		coder := global.Codecs[msg.Header.SerializeType()] // 选择解码器
		if coder == nil {
			return
		}
		inArgs := make([]interface{}, 0)
		err = coder.Decode(msg.Payload, &inArgs) //rpcdata
		if err != nil {
			log.Println("server request decode err:%v\n", err)
			return
		}

		//call local service
		handler, ok := l.Handlers[msg.ServiceClass]
		if !ok {
			log.Println("server can not found handler error:", msg.ServiceClass)
			return
		}

		l.Plugins.BeforeCall(msg.ServiceClass, msg.ServiceMethod, inArgs) //ctx

		result, err := handler.Handle(msg.ServiceMethod, inArgs)

		l.Plugins.AfterCall(msg.ServiceClass, msg.ServiceMethod, inArgs, result, err)

		//encode
		encodeRes, err := coder.Encode(result) //
		if err != nil {
			log.Printf("server response encode err:%v\n", err)
			return
		}

		//send result timeout
		if l.ServerOption.WriteTimeout != 0 {
			conn.SetWriteDeadline(startTime.Add(l.ServerOption.WriteTimeout))
		}

		l.Plugins.BeforeWrite(encodeRes)
		err = l.sendData(conn, encodeRes)
		l.Plugins.AfterWrite(encodeRes, err)
		if err != nil {
			log.Printf("server send err:%v\n", err) //timeout
			return
		}

		return
	}
}
```
# 服务注册中心：
参考开源项目实现注册中心：
应用级的服务发现：
如：`UserService.user.GetUser`
![image.png](https://cdn.nlark.com/yuque/0/2023/png/29261914/1681348475304-2d12c6c8-aeae-4fd9-87e5-047338c90d7b.png#averageHue=%23f8e4d5&clientId=u423711ee-6ac3-4&from=paste&height=336&id=ue263835d&name=image.png&originHeight=420&originWidth=898&originalType=binary&ratio=1.25&rotation=0&showTitle=false&size=87730&status=done&style=none&taskId=u39f4a850-f66c-4503-b564-b39e2fc46b6&title=&width=718.4)
Registry 处理将所有操作复制到对等 Discovery 节点以保持同步。
```go
type Discovery struct {
	config 		*configs.GlobalConfig
	client 		*httputil.Client
	Registry  	*Registry
	Nodes     	atomic.Value
	protected 	bool
}

type Registry struct {
	appmap map[string]*Apps        //key=AppID+env    -> apps
	apps   map[string]*Application //key=AppID+env

	conns     map[string]map[string]*conn // 连接池 region.zone.env.AppID-> host
	connQueue *ConnQueue
	lock      sync.RWMutex
	cLock     sync.RWMutex
	scheduler *scheduler

	gd *Guard //protect
}

// Apps app distinguished by zone
type Apps struct {
	apps            map[string]*Application
	lock            sync.RWMutex
	latestTimestamp int64
}

type Application struct {
	AppID           string
	instances       map[string]*Instance
	latestTimestamp int64
	lock            sync.RWMutex
}

type Config struct {
	Nodes []string            
	Zones map[string][]string 

	HTTPServer *ServerConfig          
	HTTPClient *httputil.ClientConfig 
	Env        *Env                   

	Scheduler  []byte                
	Schedulers map[string]*Scheduler 
}
//go:generate ..........
type Scheduler struct {
	AppID  string 
	Env    string 
	Zones  []Zone  
	Remark string 
}
```
```go
// 服务实例 Instance
type Instance struct {
	Env      string            
	AppID    string            
	Hostname string            
	Addrs    []string          
	Version  string            
	Zone     string   
    Region   string
	Metadata map[string]string 

	Status uint32 
	// timestamp
	RegTimestamp    int64 
	UpTimestamp     int64 
	RenewTimestamp  int64 
	DirtyTimestamp  int64 
	LatestTimestamp int64 
}

type InstanceInfo struct {
	Instances       map[string][]*Instance 
	Scheduler       []Zone                 
	LatestTimestamp int64                  
}

type FetchData struct {
	Instances       []*Instance 
	LatestTimestamp int64       
}
```
采用 gin 来实现一个 简易的web 服务，接受 http 请求进行服务的注册、查找、续约、下线操作，这样保障注册中心可以方便的接受来自任何语言客户端请求。
```go
POST
    api/register 	下线注册
    api/renew		心跳renew	
    api/cancel  	下线

GET
    api/fetch		获取实例
    api/fetchall	批量获取实例
	api/poll  		长轮询获取实例poll
	api/polls 		长轮询批量获取实例polls
	api/nodes		获取node节点
```
_请求参数_：appid 、 env  、 hostname 、 zone ...

### 

满足服务提供者注册/下线的能力，满足服务消费者发现/观察的能力
实现服务注册能力，先检测本地缓存查看是否已注册，没有则请求注册中心并发起注册，异步维护一个定时任务来维持心跳、并更新本地缓存，如果发生终止则会调用取消接口从注册中心注销。
开启单独协程来定期更新维护节点变化。


发现流程：（delete）

1. 选择可用的discovery节点，将appid加入poll的appid列表
2. 如果polls请求返回err，则切换node节点
3. 如果polls返回-304 ，说明appid无变更，重新发起poll监听变更
4. polls接口返回appid的instances列表，完成服务发现，根据需要选择不同的负载均衡算法进行节点的调度

### 
#### 长轮询监听
 开启长连接，轮询挂起请求，然后在有更改时写入实例，或者返回 304 NotModified。
`ConnOption` 管理超时时间，连接数
```go
type Discovery struct {
	config *configs.GlobalConfig
	client *httputil.Client

	lastHost  string
	protected bool
	Registry  *Registry
	Nodes     atomic.Value
}

ConnOption{
		TimeOut        = 30
    	MaxIdleConns   = 30               //最大连接数
    	......
}

Registry：
    EnableKeepAlives    = true  
    IdleConnTimeout     time.Duration //多长时间未使用自动关闭连接
```
`Registry`结构体内 `conn`存储数据减少连接
```go
map[string]map[string]*conn  

type conn struct {
	ch         chan map[string]*InstanceInfo //
	instanch   chan *FetchData               //
	wait       chan struct{}                 // 出队通知
	arg        *ArgPolls
	latestTime int64
	count      int // cur count
	connOption *ConnOption
}
```
`zone.env.appid` 生成 host 作为`key` ，每个host可能不止一个连接，因此使用队列排队
在实现conn排队队列时，使用了两个切片head和tail；head切片用于出队操作，tail切片用于入队操作；出队时，如果head切片为空，则交换head与tail。通过这种方式实现了底层数组空间的复用
```go
type ConnQueue struct {
	headPos int
	head    []*conn
	tail    []*conn
}

func (q *ConnQueue) pushBack(w *conn) {
    q.tail = append(q.tail, w)
}

func (q *ConnQueue) popFront() *conn {
    if q.headPos >= len(q.head) {
        if len(q.tail) == 0 {
            return nil
        }
        // 清空 Pick up tail as new head, clear tail.
        q.head, q.headPos, q.tail = q.tail, 0, q.head[:0]
    }
    w := q.head[q.headPos]
    q.head[q.headPos] = nil
    q.headPos++
    w.wait <- struct{}{}
    return w
}
```

配置中心在配置订阅时采用长轮询（long polling）方式实时监听配置变更通知
如果没有变更则timeout秒后返回304，如果有变更立即返回。
如果server节点发生变更，则接口立即返回并带上所有instances信息。如果调用失败或服务端返回非 304，则选择列表中后一个节点并进行重试直到成功并记录下当前使用的节点index。
```go
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
				ch = make(chan map[string]*InstanceInfo, 5) 
			}
			connection = newConn(ch, arg.LatestTimestamp[i], arg, connOption)
			log.Println("Polls from(%s) new connection(%d)", arg.Hostname, connection.count)
		} else {
			connection.count++ // 同一hostname上可能有多个连接
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
		}
		r.conns[k][arg.Hostname] = connection
	}
	r.cLock.Unlock()
	return
}
```
实现轮询方法 Polls，用于轮询指定应用的实例信息。

1. 首先，该方法会根据传入的参数 arg 中的appid 和最新的时间戳（**LatestTimestamp**）获取每个应用在指定环境下的实例信息，并将获取到的实例信息存入 **ins** 中。
> Polls 函数内部使用了 FetchInstanceInfo 函数，根据不同的 **LatestTimestamp** 参数获取最新的实例信息，**只有当 LatestTimestamp 参数为 0 或者小于实例信息的更新时间时，才会返回变化部分的信息。**

2. 接下来，如果有新的实例信息，则会返回实例信息。如果没有新的实例信息，则进入下一步。
3. 接着，加锁并遍历appid。对于每个appid，生成一个连接池的键值，并检查该连接池中是否存在当前主机名（arg.Hostname）的连接，如果不存在，则会创建一个新的连接并将其存储在连接池中；如果存在，则会增加该连接的计数器 count。
4. 接下来，如果连接的计数器超过了最大空闲连接数，则该连接将被添加到连接队列中。然后，该方法会启动一个 goroutine 来等待连接队列的头部连接被弹出并关闭，然后该连接会被删除。在此期间，该连接仍然会被保留，但不会被分配给新的轮询请求。如果连接队列为空，则会阻塞在 connection.wait 上等待新的连接加入队列。主要是保证对应的连接数量不超过限制。

缺点：主动遍历轮询
改进：可以设置一个变更表，标记变化节点，轮询变更表 
    可以被动更新，由变更节点发起请求(变更节点数达到一定程度或者达到时间限制)。
    可以类似Gossip，更新部分信息
    
#### 服务下线
接受服务的下线请求，并将服务从注册信息列表中删除。通过传入 env, appid, hostname 三要素信息进行对应服务实例的取消。
```go
r := model.NewRegistry()
r.Cancel(req.Env, req.AppId, req.Hostname, 0)
```

根据 appid 和 env 找到对象的 app，然后删除 app 中对应的 hostname。如果 hostname 后 app.instances 为空，那么将 app 从注册表中清除。
#### 服务续约
实现服务的健康检查机制，服务注册后，如果没有取消，那么就应该在注册表中，可以随时查到，如果某个服务实例挂了，能自动的从注册表中删除，保障注册表中的服务实例都是正常的。
服务实例（客户端）主动上报，调用续约接口进行续约，续约设有时效 TTL。而不是注册中心（服务端）主动探活 
> 服务实例主动上报并续约的优点：
> 1. 实时性更高：服务实例在过期之前主动向注册中心发送续约请求，可以保证实例的状态实时性更高，让消费者能够更快地发现和使用服务实例。
> 2. 减少网络开销：服务实例向注册中心发送续约请求，可以避免因为长时间不发起请求而导致的网络超时等问题。
> 3. 避免误判：服务实例主动发送续约请求，可以避免因为网络故障等原因，导致服务实例被错误地认为下线的情况。
> 
服务注册中心主动探活的优点：
> 1. 安全性更高：服务注册中心主动向服务实例发送探活请求，可以避免服务实例出现问题而无法续约的情况，保证服务的可靠性。
> 2. 节省资源：注册中心主动向服务实例发送探活请求，可以减少服务实例在续约上的资源开销，尤其是当服务实例数量很大时，服务实例主动上报和续约会占用较多的系统资源。
> 
因此，服务实例主动上报并续约更适合在服务实例数量较小、网络较为稳定的情况下使用，而注册中心主动探活则更适合在服务实例数量较大、网络较为不稳定的情况下使用。

根据 appid 和 env 找到对象的 app，再根据 hostname 找到对应主机实例instance，更新其 RenewTimestamp 为当前时间。
```go
r := model.NewRegistry()
r.Renew(renewCtx,req.Env, req.AppId, req.Hostname)
```
根据http请求，进行renew
#### 服务剔除
如果服务因为网络故障或挂了导致不能提供服务，可以通过检查它是否按时续约来判断，把 TTL 达到阈值的服务实例剔除（Cancel），实现服务的被动下线。
首先在新建注册表时开启一个定时任务，新启一个 goroutine 来实现。
```go
func NewRegistry() *Registry {
    ........
    go r.evict()
}
```

配置定时检查的时间间隔，默认 60 秒，通过 Tick 定时器开启 evict。
遍历注册表的所有 apps，然后再遍历其中的 instances，如果当前时间减去实例上一次续约时间 instance.RenewTimestamp 达到阈值（默认 90 秒），那么将其加入过期队列中。这里并没有直接将过期队列所有实例都取消，考虑 GC 因素，设定了一个剔除的上限 evictionLimit，随机剔除一些过期实例。
剔除上限数量，是通过当前注册表大小（注册表所有 instances 实例数）减去 触发自我保护机制的阈值（当前注册表大小 * 保护自我机制比例值）。
剔除过期时，随机剔除。随机剔除能最大程度保障，剔除的实例均匀分散到所有应用实例中，降低某服务被全部清空的风险。随机剔除Knuth-Shuffle算法实现：循环遍历过期列表，将当前数与特定随机数交换。如果只是简单的从列表中取第一个或最后一个过期实例进行驱逐，可能会导致某些实例因为位置原因被过度保护而得不到驱逐，或者某些实例总是被优先驱逐而无法保持服务的稳定性。

#### 自我保护
**功能目标：**假设一种情况，网络一段时间发生了异常，所有服务都没成功续约，不能将所有服务全部剔除。需要一个自我保护的机制防止此类事情的发生。
按短时间内**失败的比例达到某特定阈值就开启保护**，**保护模式下不进行服务剔除**。所以我们需要一个统计模块，续约成功 +1。默认情况下，服务剔除每 60 秒执行一次，服务续约每 30 秒执行一次，那么一个服务实例在检查时应该有 2 次续约。
最后增加一个定时器，如果超过一定时间（如15 分钟），重新计算下当前实例数，重置保护阈值，降低脏数据风险。
```go
type Guard struct {
    renewCount     int64    记录所有服务续约次数，每执行一次 renew 加 1
    lastRenewCount int64	记录上一次检查周期（默认 60 秒）服务续约统计次数
    needRenewCount int64  	记录一个周期总计需要的续约数，按一次续约 30 秒，一周期 60 秒，一个实例就需要 2 次，所以服务注册时 + 2，服务取消时 - 2
    threshold      int64	服务续约比例 ：通过 needRenewCount  和阈值比例 （0.xx）确定触发自我保护的值
    lock           sync.RWMutex
}
```
#### 集群实现
实现点对点的架构方式，通过复制技术实现各副本间一致性问题
P2P点对点架构
每个节点 Node 互相独立，并通过数据复制同步数据，每个节点都可接受服务注册、续约和发现操作。
一个节点Node就是一个独立的注册中心服务，集群由多个节点Node组成。结构体 Node 存储节点地址和节点状态，节点状态有两种：上线状态、下线状态。
将节点作为服务实例，实现自发现。注册中心集群当做服务 Application，如 将AppId  统一命名为 my.discovery
启动服务初始化 Discovery 时，将自己注册到注册中心。注册成功后同步给集群其他节点。注册成功定期续约。
开启定时任务，进行节点的状态变更感知，维护集群节点的上下线，从节点注册表中拉取 AppId 为 my.discovery(注册中心) 的数据，然后通过该数据中的实例信息来维护节点列表。
同步：当前节点依次广播
```go
type Nodes struct {
	nodes    []*Node
	zones    map[string][]*Node
	selfAddr string
}

type Node struct {
	config      *Config
	addr        string
	status      int
	registerURL string
	cancelURL   string
	renewURL    string
    .......
	zone        string
}
```
数据副本同步与数据一致性
使用 AP 模型，尽量保障最终一致性

- 服务启动时当前节点数据为空，需要同步其他节点数据
- 某节点接收到服务最新数据变更，需要同步给其他节点
> 对于服务注册来说，针对同一个服务，即使注册中心的不同节点保存的服务注册信息不相同，也并不会造成灾难性的后果，对于服务消费者来说，能消费才是最重要的，就算拿到的数据不是最新的数据，消费者本身也可以进行尝试失败重试。总比为了追求数据的一致性而获取不到实例信息整个服务不可用要好。
> 所以，对于服务注册来说，可用性比数据一致性更加的重要，选择AP。

当实例向某节点发起注册、续约、取消操作，该节点在完成本地注册表数据更新后，还需要将其同步给其他节点。同步采用当前节点依次广播的形式 
采用了弱一致性策略，而并没有采用同步写模式  （要实现强一致性，则需要在数据写入当前节点并完成同步之前，所有节点数据不可读或仍读取之前版本数据（快照/多版本控制））。 
同步失败不做处理：

- 如果是续约事件丢失，可以在下一次续约时补上；
- 如果是注册事件丢失，也可以在下次续约时发现并修复；
- 如果是取消事件丢失，长时间不续约会有剔除。

同时通过记录失败队列，补发失败请求来快速修复。 失败队列结构和上述 Failback 失败重试队列一样。

和gossip 比，缺点：

- 消息冲突：由于每个节点都可以与其他任意节点通信，如果同时有多个节点发送广播消息，就可能会导致信道上出现消息冲突，从而影响消息的正确传输。
- 消息冗余：由于每个节点都要接收所有的广播消息（gossip的谣言传播只是发送新数据），就可能会造成大量的消息冗余，浪费网络带宽和节点资源。
- 消息延迟：由于每个节点都要处理所有的广播消息，就可能会造成消息处理的延迟，影响网络的响应速度和实时性。

注册表初始化：
节点Node首次启动时，遍历所有节点，获取注册表数据，依次注册到本地。只有当所有数据同步完毕后，该注册中心才可对外提供服务，切换为上线状态。
注册表数据变更时同步：
遍历所有的节点，依次执行注册操作。
最终一致性：
开启定时任务，定期与其他节点进行数据比对，并根据 lastTimestamp 来决策哪条数据准确进行修复，如果数据不存在需要进行补录。当所有节点都和其他节点进行比对并修复后，理论上可达成一致性。


例：
```go
静态配置集群节点列表
nodes: ["localhost:8001", "localhost:8002", "localhost:8002"]
http_server: "localhost:8001" 
hostname: "host1"  

nodes: ["localhost:8001", "localhost:8002", "localhost:8002"]
http_server: "localhost:8002" 
hostname: "host2" 

nodes: ["localhost:8001", "localhost:8002", "localhost:8002"]
http_server: "localhost:8002" 
hostname: "host3" 
```
启动节点1，节点1首先从配置中获取集群列表，但是节点感知后发现其他两个节点无法访问，将节点1改为单节点。
启动节点2，节点2先同步节点1的注册中心，注册自己并同步到节点1，节点1和节点2组成双节点集群，数据相互复制保持一致性。 启动节点 3 也是如此。
注销 node 3，会发现在node 1和node 2上已经被注销了。




