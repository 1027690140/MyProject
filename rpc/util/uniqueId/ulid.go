package uniqueid

import (
	"fmt"
	"math/rand"
	"rpc_service/global"
	"sync"
	"time"

	"github.com/go-redis/redis"
	"github.com/oklog/ulid"
)

var rdb = global.RedisClient

// 阈值，当 Redis 中的 ULID 数量低于这个值时，会自动生成 ULID
var ULIDthreshold = 100

var ULIDQueue = &FIFOQueue{queue: make([]string, 0)}

type FIFOQueue struct {
	sync.Mutex
	queue []string
}

func (q *FIFOQueue) Push(items ...string) {
	q.Lock()
	defer q.Unlock()
	q.queue = append(q.queue, items...)
}

func (q *FIFOQueue) Pop() (string, bool) {
	q.Lock()
	if len(q.queue) == ULIDthreshold/50 {
		// 无法从队列中取出 ULID
		ulid, err := takeULIDFromRedis(rdb, "ULID", q)
		q.Unlock() // 在调用 takeULIDFromRedis 之前释放锁
		if err != nil {
			fmt.Println("Failed to take ULID from Redis:", err)
			// 从 Redis 中取出 ULID 放到队列中
			ulids, err := takeBatchULIDsFromRedis(rdb, "ULID", ULIDthreshold)
			if err != nil {
				fmt.Println("Failed to take ULIDs from Redis:", err)
				return "", false
			}
			q.Push(ulids...) // 在主线程中修改队列
			return "", false
		}
		// 从 Redis 中取出 ULID 放到队列中
		ulids, err := takeBatchULIDsFromRedis(rdb, "ULID", ULIDthreshold)
		if err != nil {
			fmt.Println("Failed to take ULIDs from Redis:", err)
			return "", false
		}
		q.Push(ulids...) // 在主线程中修改队列
		return ulid, true
	}

	item := q.queue[0]
	q.queue = q.queue[1:]
	q.Unlock() // 在从队列中取出 ULID 之后释放锁
	return item, true
}

// 检查 ULID 数量并自动生成
func checkAndGenerateULIDs(rdb *redis.Client, redisKey string, threshold int, ULIDthreshold int, fifoQueue *FIFOQueue) {
	ulidCount, err := rdb.ZCard(redisKey).Result()
	if err != nil {
		fmt.Println("Failed to check ULID count in Redis:", err)
		return
	}

	if ulidCount < int64(ULIDthreshold) {
		fmt.Println("ULID count is below the threshold. Generating ULIDs...")

		// 生成指定数量的 ULID
		for i := 0; i < threshold; i++ {
			ulid := generateULID()
			err := rdb.ZAdd(redisKey, redis.Z{Score: float64(time.Now().UnixNano()), Member: ulid}).Err()
			if err != nil {
				fmt.Println("Failed to store ULID in Redis:", err)
				continue
			}
			fifoQueue.Push(ulid)
		}

		fmt.Printf("Generated and stored %d ULIDs in Redis.\n", threshold)
	} else {
		fmt.Println("ULID count is above the threshold. No generation needed.")
	}
}

// 从 Redis 中取走一个 ULID
func takeULIDFromRedis(rdb *redis.Client, redisKey string, FIFOQueue *FIFOQueue) (string, error) {
	result, err := rdb.ZPopMin(redisKey).Result()
	if err != nil {
		return "", err
	}

	ulid := result[0].Member.(string)

	// 如果 Redis 中的 ULID 数量低于阈值，生成新的 ULID
	go checkAndGenerateULIDs(rdb, redisKey, ULIDthreshold, ULIDthreshold, FIFOQueue)

	return ulid, nil
}

// 从 Redis 中取出一批 ULID
func takeBatchULIDsFromRedis(rdb *redis.Client, redisKey string, batchSize int) ([]string, error) {
	ulids, err := rdb.ZRange(redisKey, 0, int64(batchSize-1)).Result()
	if err != nil {
		return nil, err
	}

	go func() {
		_, err = rdb.ZRemRangeByRank(redisKey, 0, int64(batchSize-1)).Result()
		if err != nil {
			fmt.Errorf("Failed to remove ULIDs from Redis: %v", err)
			return
		}
	}()

	return ulids, nil
}

// 生成 ULID
func generateULID() string {
	entropy := ulid.Monotonic(rand.New(rand.NewSource(time.Now().UnixNano())), 0)
	id := ulid.MustNew(ulid.Timestamp(time.Now()), entropy)
	return id.String()
}
