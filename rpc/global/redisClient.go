package global

import (
	"rpc_service/config"

	"github.com/go-redis/redis"
)

// Redis 客户端对象
var RedisClient *redis.Client
var RedisConfig config.RedisConfig
var CacheExpireTime int64

// 初始化 Redis 客户端
func init() {
	redisConfig := config.LoadRedisConfig() // 加载 Redis 配置

	RedisClient := redis.NewClient(&redis.Options{
		Addr:     RedisConfig.Address,
		Password: RedisConfig.Password,
		DB:       RedisConfig.DB,
	})

	// 检查与 Redis 服务器的连接是否正常
	_, err := RedisClient.Ping().Result()
	if err != nil {
		panic(err)
	}

	CacheExpireTime = redisConfig.CacheExpireTime
}
