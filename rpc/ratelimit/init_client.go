package ratelimit

import "github.com/go-redis/redis"

func init() {
	client = redis.NewClient(&redis.Options{
		Addr:       "localhost:6379",
		Password:   "",
		DB:         0,
		PoolSize:   3,
		MaxRetries: 3,
	})
	_, err := client.Ping().Result()
	if err != nil {
		panic(err)
	}
}
