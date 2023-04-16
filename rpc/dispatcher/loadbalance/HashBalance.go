package loadbalance

import (
	"hash/crc32"
	"sort"
	"strconv"
	"sync"
)

/*
求余hash算法：hash(object)%N
一致性hash算法：hash(object)%2^32
根据每一台服务器不同的 ip:port 根据自己的 key生成算法 ，生成一个唯一的 key 值
key => ip:port 把机器唯一的 key 映射机器访问地址 ip:port 设置到一个 有序的循环圆 上
请求过来的时候，根据请求内容生成按 自己的 key生成算法 也生成一个 请求的 key
请求的 key 和 有序的循环圆 上 机器的 key 循环对比， 第一个 机器的 key 大于 请求的 key 就是最优解，由它来处理该请求
如果 请求的 key 比 有序的循环圆 上 机器的 key 都大，那么由 圆上第一条机器 处理
有序的循环圆 上机器，根据访问情况。近实时增删机器映射
*/

type errorString struct {
	s string
}

func (e *errorString) Error() string {
	return "HashBalancerError: " + e.s
}

// 定义错误类型
func HashBalancerError(text string) error {
	return &errorString{text}
}

// 定义环类型
type Circle []uint32

func (c Circle) Len() int {
	return len(c)
}

func (c Circle) Less(i, j int) bool {
	return c[i] < c[j]
}

func (c Circle) Swap(i, j int) {
	c[i], c[j] = c[j], c[i]
}

type Hash func(date []byte) uint32

type HashBalancer struct {
	hash         Hash              // 产生uint32类型的函数
	circle       Circle            // 环
	virtualNodes int               // 虚拟节点个数
	virtualMap   map[uint32]string // 点到主机的映射
	members      map[string]bool   // 主机列表
	sync.RWMutex
}

// just for interface{}
func (c *HashBalancer) Add(...string) error
func (c *HashBalancer) Get() (string, error)

func NewHashBalancer() *HashBalancer {
	return &HashBalancer{
		hash:         crc32.ChecksumIEEE,
		circle:       Circle{},
		virtualNodes: 150,
		virtualMap:   make(map[uint32]string),
		members:      make(map[string]bool),
	}
}

// generate a string key for an element with an index
func (c *HashBalancer) eltKey(key string, idx int) string {
	return key + "|" + strconv.Itoa(idx)
}

func (c *HashBalancer) updateCricle() {
	c.circle = Circle{}
	for k := range c.virtualMap {
		c.circle = append(c.circle, k)
	}
	sort.Sort(c.circle)
}

func (c *HashBalancer) Members() []string {
	c.RLock()
	defer c.RUnlock()

	m := make([]string, len(c.members))

	var i = 0
	for k := range c.members {
		m[i] = k
		i++
	}

	return m
}

func (c *HashBalancer) GetByKey(key string) string {
	hashKey := c.hash([]byte(key))
	c.RLock()
	defer c.RUnlock()

	i := c.search(hashKey)

	return c.virtualMap[c.circle[i]]
}

// 在 key 附近搜索附近的 vnode
// sort.Search 使用二进制搜索来查找键
// 每个 vnode 覆盖它自己和顺时针方向的区域
func (c *HashBalancer) search(key uint32) int {
	f := func(x int) bool {
		return c.circle[x] >= key
	}

	i := sort.Search(len(c.circle), f)
	i = i - 1
	if i < 0 {
		i = len(c.circle) - 1
	}
	return i
}

// this function is beautiful
func (c *HashBalancer) ForceSet(keys ...string) {
	mems := c.Members()
	for _, elt := range mems {
		var found = false

	FOUNDLOOP:
		for _, k := range keys {
			if k == elt {
				found = true
				break FOUNDLOOP
			}
		}
		if !found {
			c.Remove(elt)
		}
	}

	for _, k := range keys {
		c.RLock()
		_, ok := c.members[k]
		c.RUnlock()

		if !ok {
			c.AddByKey(k)
		}
	}
}

func (c *HashBalancer) AddByKey(key string) {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.members[key]; ok {
		return
	}

	c.members[key] = true

	for idx := 0; idx < c.virtualNodes; idx++ {
		c.virtualMap[c.hash([]byte(c.eltKey(key, idx)))] = key
	}

	c.updateCricle()
}

func (c *HashBalancer) Remove(elt string) {
	c.Lock()
	defer c.Unlock()

	if _, ok := c.members[elt]; !ok {
		return
	}

	delete(c.members, elt)

	for idx := 0; idx < c.virtualNodes; idx++ {
		delete(c.virtualMap, c.hash([]byte(c.eltKey(elt, idx))))
	}

	c.updateCricle()
}
