package storage

import (
	"context"
	"service_discovery/configs"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoDB客户端
var mongoClient *mongo.Client

var databaseName string
var collectionName string
var instanceCollectionName string

var ApplyURI string

// 初始化MongoDB客户端和相关配置
func InitMongoClient() {
	mongoConfig, err := configs.LoadMongoConfig() // 从配置文件中加载MongoDB配置
	if err != nil {
		panic(err)
	}

	client, err := mongo.Connect(context.TODO(), options.Client().ApplyURI(mongoConfig.URI))
	if err != nil {
		panic(err)
	}

	err = client.Ping(context.TODO(), nil)
	if err != nil {
		panic(err)
	}

	mongoClient = client
	databaseName = mongoConfig.Database
	collectionName = mongoConfig.Collection
	instanceCollectionName = mongoConfig.InstanceCollection
}
