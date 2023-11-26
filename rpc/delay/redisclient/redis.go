package redisclient

import (
	"rpc_service/config"

	"github.com/go-redis/redis"
)

var rc config.RedisConfig
var RedisClient = redis.NewClient(&redis.Options{
	Addr:     rc.Address,
	Password: rc.Password,
	DB:       rc.DB,
})

func ZAdd(key string, score int, data string) error {
	z := redis.Z{Score: float64(score), Member: data}
	_, err := RedisClient.ZAdd(key, z).Result()
	return err
}

func ZRangeFirst(key string) ([]interface{}, error) {
	val, err := RedisClient.ZRangeWithScores(key, 0, 0).Result()
	if err != nil {
		return nil, err
	}
	if len(val) == 1 {
		score := val[0].Score
		member := val[0].Member
		data := []interface{}{score, member}
		return data, nil
	}
	return nil, nil
}

func ZRem(key string, member string) error {
	_, err := RedisClient.ZRem(key, member).Result()
	return err
}

func Set(key string, data string) error {
	_, err := RedisClient.Set(key, data, -1).Result()
	return err
}

func Get(key string) (string, error) {
	val, err := RedisClient.Get(key).Result()
	return val, err
}

func Del(key string) error {
	_, err := RedisClient.Del(key).Result()
	return err
}
