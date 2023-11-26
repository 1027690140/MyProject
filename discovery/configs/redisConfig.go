package configs

import (
	"github.com/spf13/viper"
)

type RedisConfig struct {
	Address         string
	Password        string
	DB              int
	CacheExpireTime int64
}

// 加载 Redis 配置
func LoadRedisConfig() RedisConfig {
	viper.SetConfigFile("config.yaml") // 配置文件是 config.yaml
	err := viper.ReadInConfig()
	if err != nil {
		panic(err)
	}

	var redisConfig RedisConfig
	err = viper.UnmarshalKey("redis", &redisConfig)
	if err != nil {
		panic(err)
	}

	return redisConfig
}
