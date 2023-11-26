package trace

import (
	"context"
	"encoding/json"
	"fmt"
	"rpc_service/global"
	"time"

	"github.com/go-redis/redis"
)

const (
	redisKey    = "traceinfo"
	maxRedisLen = 1000 // Redis中存储的最大traceinfo数量
	mongoDBName = "traceinfo"
	collection  = "traces"
	threshold   = 100000
)

// ArchiveAndPersistTraceInfo 持久化traceinfo到redis和MongoDB
func ArchiveAndPersistTraceInfo(traceinfo *TraceInfo) error {
	// 获取traceid
	traceID := traceinfo.TraceID

	// 构建Redis中的Zset名称
	redisKey := traceID
	var err error
	// 将traceinfo序列化为JSON字符串
	traceJSON, err := json.Marshal(traceinfo)
	if err != nil {
		return fmt.Errorf("failed to serialize traceinfo: %v", err)
	}

	// 构建Redis中的成员标识
	member := fmt.Sprintf("%d", time.Now().UnixNano())

	// 将traceinfo存储到Redis的Zset中
	err = redisClient.ZAdd(redisKey, redis.Z{
		Score:  float64(traceinfo.EditTimestamp),
		Member: member,
	}).Err()
	if err != nil {
		return fmt.Errorf("failed to persist traceinfo to Redis: %v", err)
	}

	// 将traceJSON存储到Redis的Zset中
	err = redisClient.Set(member, traceJSON, 0).Err()
	if err != nil {
		return fmt.Errorf("failed to persist traceJSON to Redis: %v", err)
	}
	// 检查Redis中traceinfo的数量
	listLen, err := redisClient.ZCard(redisKey).Result()
	if err != nil {
		return fmt.Errorf("failed to get Redis list length: %v", err)
	}

	// 如果达到了持久化到MongoDB的条件
	if listLen >= maxRedisLen {
		// 从Redis中获取traceinfo的成员列表
		traceMembers, err := redisClient.ZRange(redisKey, 0, maxRedisLen-1).Result()
		if err != nil {
			return fmt.Errorf("failed to get traceinfo members from Redis: %v", err)
		}

		// 创建MongoDB的文档对象列表
		var documents []interface{}
		for _, member := range traceMembers {
			// 从Redis中获取traceinfo的JSON字符串
			traceJSON, err := redisClient.Get(member).Result()
			if err != nil {
				return fmt.Errorf("failed to get traceinfo JSON from Redis: %v", err)
			}

			// 解析traceinfo的JSON字符串
			var traceinfo TraceInfo
			err = json.Unmarshal([]byte(traceJSON), &traceinfo)
			if err != nil {
				return fmt.Errorf("failed to deserialize traceinfo: %v", err)
			}

			// 设置MongoDB文档的traceid字段
			traceinfo.TraceID = traceID

			// 将traceinfo添加到文档列表中
			documents = append(documents, traceinfo)
		}

		// 将traceinfo持久化到MongoDB
		mongoCollection := mongoClient.Database(mongoDBName).Collection(collection)
		_, err = mongoCollection.InsertMany(context.Background(), documents)
		if err != nil {
			return fmt.Errorf("failed to persist traceinfo to MongoDB: %v", err)
		}

		// 从Redis中删除已持久化的traceinfo
		// 将[]string转换为[]interface{}
		var traceMembersInterface = make([]interface{}, len(traceMembers))
		for i, v := range traceMembers {
			traceMembersInterface[i] = v
		}
		// 从Redis中删除已持久化的traceinfo
		_, err = redisClient.ZRem(redisKey, traceMembersInterface...).Result()
		if err != nil {
			return fmt.Errorf("failed to remove traceinfo from Redis: %v", err)
		}
	}

	return nil
}

// GetLatestTraceInfoFromRedis redis Zset里面最新的一个traceinfo
func GetLatestTraceInfoFromRedis(redisKey string) (*TraceInfo, *global.StatusError) {
	// Get the latest member (JSON string) from the Zset with the highest score (EditTimestamp)
	traceJSON, err := redisClient.ZRevRange(redisKey, 0, 0).Result()
	if err != nil {
		return nil, &global.StatusError{Message: fmt.Sprintf("failed to get the latest traceinfo from Redis: %v", err)}

	}

	if len(traceJSON) == 0 {
		// No traceinfo found in Redis
		return nil, nil
	}

	// Unmarshal the JSON string into a TraceInfo struct
	var latestTrace TraceInfo
	err = json.Unmarshal([]byte(traceJSON[0]), &latestTrace)
	if err != nil {
		return nil, &global.StatusError{Message: fmt.Sprintf("ailed to deserialize traceinfo from Redis : %v", err)}
	}

	go func() {
		// 最久的traceinfo 更新时间如果大于阈值，也持久化
		if time.Since(time.Unix(0, latestTrace.EditTimestamp)) > threshold {
			// Persist the latestTrace to MongoDB
			err := PersistTraceInfoToMongoDB(&latestTrace)
			if err != nil {
				fmt.Errorf("failed to persist traceinfo to MongoDB: %v", err)
				return
			}

			// Remove the latestTrace from Redis
			_, err2 := redisClient.ZRem(redisKey, traceJSON[0]).Result()
			if err2 != nil {
				fmt.Errorf("failed to remove traceinfo from Redis: %s", err)
				return
			}
		}
	}()

	return &latestTrace, nil
}

// PersistTraceInfoToMongoDB 持久化
func PersistTraceInfoToMongoDB(traceinfo *TraceInfo) *global.StatusError {
	// Convert traceinfo to JSON
	traceJSON, err := json.Marshal(traceinfo)
	if err != nil {
		return &global.StatusError{Message: fmt.Sprintf("failed to serialize traceinfo : %v", err)}
	}

	// Persist traceinfo to MongoDB
	mongoCollection := mongoClient.Database(mongoDBName).Collection(collection)
	_, err = mongoCollection.InsertOne(context.Background(), traceJSON)
	if err != nil {
		return &global.StatusError{Message: fmt.Sprintf("failed to persist traceinfo to MongoDB: %v", err)}
	}

	return nil
}
