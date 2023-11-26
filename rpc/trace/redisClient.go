package trace

import (
	"rpc_service/config"

	"github.com/go-redis/redis"
)

// Redis 客户端对象
var redisClient *redis.Client
var redisConfig config.RedisConfig

var cacheExpireTime int64

// 初始化 Redis 客户端
func InitRedisClient() {
	redisConfig := config.LoadRedisConfig() // 加载 Redis 配置

	redisClient := redis.NewClient(&redis.Options{
		Addr:     redisConfig.Address,
		Password: redisConfig.Password,
		DB:       redisConfig.DB,
	})

	// 检查与 Redis 服务器的连接是否正常
	_, err := redisClient.Ping().Result()
	if err != nil {
		panic(err)
	}

	cacheExpireTime = redisConfig.CacheExpireTime
}
