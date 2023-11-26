package pool

import (
	"container/list"
	"sync"
	"time"
)

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

func NewLRU(maxEntries int, idleTimeout time.Duration) *LRU {
	return &LRU{
		activeList:   list.New(),
		inactiveList: list.New(),
		addrMap:      make(map[string]*list.Element),
		lastUsedTime: make(map[string]time.Time),
		maxEntries:   maxEntries,
		idleTimeout:  idleTimeout,
		lruEvitChan:  make(chan struct{}, 1),
	}
}

func (lru *LRU) Add(addr string) {
	lru.lock.RLock()
	defer lru.lock.RUnlock()
	// 如果元素已经存在,直接移到活跃链表头部并更新最后使用时间
	if node, ok := lru.addrMap[addr]; ok {
		lru.activeList.MoveToFront(node)
		lru.lastUsedTime[addr] = time.Now()
		return
	}

	// 如果元素不存在，将其加入不活跃链表头部并更新最后使用时间
	node := lru.inactiveList.PushFront(addr)
	lru.addrMap[addr] = node
	lru.lastUsedTime[addr] = time.Now()

	// 如果不活跃链表元素超过了最大元素个数，缩容最久未使用的元素
	if lru.inactiveList.Len() > lru.maxEntries {
		if len(lru.lruEvitChan) == 0 {
			lru.lruEvitChan <- struct{}{}
		}
	}
}

// GetOldest 用于缩容最久未使用的元素
func (lru *LRU) GetOldest(num int) []string {
	lru.lock.RLock()
	defer lru.lock.RUnlock()
	res := make([]string, num)
	for i, node := 0, lru.inactiveList.Back(); node != nil && i < num; node, i = node.Prev(), i+1 {
		res = append(res, node.Value.(string))
		next := node.Prev()
		// 从不活跃链表中移除节点 ,防止重复缩容
		lru.inactiveList.Remove(node)
		node = next
	}
	return res
}

// GetNewest 用于扩容最近使用的元素
func (lru *LRU) GetNewest(num int) []string {
	lru.lock.RLock()
	defer lru.lock.RUnlock()

	res := make([]string, 0, num)
	if lru.activeList.Len() < num {
		// activeList中的节点数量不足num个，只需要返回所有的节点即可
		for node := lru.activeList.Front(); node != nil; node = node.Next() {
			res = append(res, node.Value.(string))
		}
	} else {
		// 获取activeList前num个节点，并将这些节点移动到activeList尾部
		for i, node := 0, lru.activeList.Front(); node != nil && i < num; node, i = node.Next(), i+1 {
			res = append(res, node.Value.(string))
			lru.activeList.MoveToBack(node)
		}
	}
	return res
}

func (lru *LRU) removeNode(node *list.Element) {
	lru.lock.RLock()
	defer lru.lock.RUnlock()
	addr := node.Value.(string)
	delete(lru.addrMap, addr)
	lru.inactiveList.Remove(node)
}

// RunIdleCheck 用于定时检查不活跃链表中的元素是否超时，如果超时则通知缩容
func (lru *LRU) RunIdleCheck(idleTimeout time.Duration) {
	ticker := time.NewTicker(idleTimeout / 2) // 每 idleTimeout / 2 触发一次
	defer ticker.Stop()
	for {

		select {
		case <-lru.lruEvitChan:
		case <-ticker.C:
			current := time.Now()
			lru.lock.RLock()
			element := lru.activeList.Back()
			// 遍历 activeList，将超时未使用的节点移到 inactiveList
			for element != nil {
				addr := element.Value.(string)
				lastUsedTime := lru.lastUsedTime[addr]
				if current.Sub(lastUsedTime) > idleTimeout {
					// 将节点移到 inactiveList
					next := element.Prev()
					lru.activeList.Remove(element)
					lru.inactiveList.PushFront(addr)
					element = next
				} else {
					// 继续遍历 activeList
					element = element.Prev()
				}
			}
			element = lru.inactiveList.Back()
			// 遍历 inactiveList，将超时未使用的节点通知缩容并删除节点
			for element != nil {
				addr := element.Value.(string)
				lastUsedTime := lru.lastUsedTime[addr]
				if current.Sub(lastUsedTime) > idleTimeout {
					// 通知缩容
					lru.Pools.connPools[addr].keepAliveChan <- struct{}{}
					delete(lru.lastUsedTime, addr)

					// 删除当前节点并获取下一个节点,防止重复缩容
					next := element.Prev()
					lru.inactiveList.Remove(element)
					delete(lru.addrMap, addr)
					element = next
				} else {
					// 继续遍历 inactiveList
					element = element.Prev()
				}
			}

			lru.lock.RUnlock()

		case <-lru.Pools.poolClosedChan:
			return
		}
	}
}
