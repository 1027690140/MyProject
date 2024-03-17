package storage

import (
	"context"
	"fmt"
	"service_discovery/model"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	noedVersion uint64 // 节点版本号
)

func GenerateNodeVersion() string {
	noedVersion := time.Now().UnixNano()
	versionString := fmt.Sprintf("v%d", noedVersion)
	return versionString
}

// MongoDB的文档结构匹配
type Instance struct {
	Env             string            `bson:"env"`
	AppID           string            `bson:"app_id"`
	Hostname        string            `bson:"hostname"`
	Addrs           []string          `bson:"addrs"`
	Version         string            `bson:"version"`
	Zone            string            `bson:"zone"`
	Region          string            `bson:"region"`
	Labels          []string          `bson:"labels"`
	Metadata        map[string]string `bson:"metadata"`
	Status          uint32            `bson:"status"`
	RegTimestamp    int64             `bson:"reg_timestamp"`
	UpTimestamp     int64             `bson:"up_timestamp"`
	RenewTimestamp  int64             `bson:"renew_timestamp"`
	DirtyTimestamp  int64             `bson:"dirty_timestamp"`
	LatestTimestamp int64             `bson:"latest_timestamp"`
}

// MongoDB的文档结构匹配
type Node struct {
	Addr        string `bson:"addr"`
	RegisterURL string `bson:"register_url"`
	CancelURL   string `bson:"cancel_url"`
	RenewURL    string `bson:"renew_url"`
	PollURL     string `bson:"poll_url"`
	PollsURL    string `bson:"polls_url"`
	Zone        string `bson:"zone"`
	Region      string `bson:"region"`
}

type NodeDocument struct {
	Version string `bson:"version"` // 版本号
	Node    Node   `bson:"node"`    // 节点信息
}

// 获取MongoDB的Collection对象
func getMongoCollection(instance model.Instance) (*mongo.Collection, error) {
	if mongoClient == nil {
		return nil, fmt.Errorf("MongoDB client not initialized")
	}

	database := mongoClient.Database(databaseName)
	collection := database.Collection(instance.Hostname)
	return collection, nil
}

// 存储实例到MongoDB
func storeInstance(instance model.Instance) error {
	collection, err := getMongoCollection(instance)
	if err != nil {
		return err
	}

	_, err = collection.InsertOne(context.TODO(), instance)
	if err != nil {
		return err
	}

	fmt.Println("Instance stored successfully")
	return nil
}

// 从MongoDB中获取注册中心的集群节点信息
func GetClusterNodesFromMongoDB() (*model.Nodes, error) {
	if mongoClient == nil {
		return nil, fmt.Errorf("MongoDB client not initialized")
	}

	collection := mongoClient.Database(databaseName).Collection(collectionName)

	// 执行查询操作
	cursor, err := collection.Find(context.Background(), bson.D{})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(context.Background())

	nodes := &model.Nodes{
		Nodes:    make([]*model.Node, 0),
		Zones:    make(map[string][]*model.Node),
		SelfAddr: "", // 根据实际情况设置 SelfAddr
		Zone:     "", // 根据实际情况设置 Zone
	}

	for cursor.Next(context.Background()) {
		var nodeDoc NodeDocument
		if err := cursor.Decode(&nodeDoc); err != nil {
			return nil, err
		}
		node := &model.Node{
			Addr:        nodeDoc.Node.Addr,
			RegisterURL: nodeDoc.Node.RegisterURL,
			CancelURL:   nodeDoc.Node.CancelURL,
			RenewURL:    nodeDoc.Node.RenewURL,
			PollURL:     nodeDoc.Node.PollURL,
			PollsURL:    nodeDoc.Node.PollsURL,
			Zone:        nodeDoc.Node.Zone,
		}

		nodes.Nodes = append(nodes.Nodes, node)

		if _, ok := nodes.Zones[node.Zone]; !ok {
			nodes.Zones[node.Zone] = make([]*model.Node, 0)
		}
		nodes.Zones[node.Zone] = append(nodes.Zones[node.Zone], node)
	}

	return nodes, nil
}

// StoreClusterNodesToMongoDB 将注册中心集群节点信息存储到MongoDB
func StoreClusterNodesToMongoDB(nodes *model.Nodes) error {
	if mongoClient == nil {
		return fmt.Errorf("MongoDB client not initialized")
	}

	// 清空集合中的文档
	if err := clearCollection(); err != nil {
		return err
	}

	// 将节点信息按区域分组
	nodesByZone := make(map[string][]*model.Node)
	for _, node := range nodes.Nodes {
		if _, ok := nodesByZone[node.Zone]; !ok {
			nodesByZone[node.Zone] = make([]*model.Node, 0)
		}
		nodesByZone[node.Zone] = append(nodesByZone[node.Zone], node)
	}

	// 逐个区域存储节点信息
	for zone, zoneNodes := range nodesByZone {
		// 获取该区域的集合名称
		collectionName := getCollectionName(zone)

		// 获取或创建对应的集合

		collection := mongoClient.Database(databaseName).Collection(collectionName)

		// 将节点信息转换为文档列表
		documents := make([]interface{}, len(zoneNodes))
		for i, node := range zoneNodes {
			document := NodeDocument{
				Version: GenerateNodeVersion(), // 设置实际的版本号
				Node: Node{
					Addr:        node.Addr,
					RegisterURL: node.RegisterURL,
					CancelURL:   node.CancelURL,
					RenewURL:    node.RenewURL,
					PollURL:     node.PollURL,
					PollsURL:    node.PollsURL,
					Zone:        node.Zone,
				},
			}
			documents[i] = document
		}

		// 执行插入操作
		_, err := collection.InsertMany(context.Background(), documents)
		if err != nil {
			return err
		}
	}

	return nil
}

// 清空集合中的文档
func clearCollection() error {
	if mongoClient == nil {
		return fmt.Errorf("MongoDB client not initialized")
	}

	collection := mongoClient.Database(databaseName).Collection(collectionName)

	_, err := collection.DeleteMany(context.Background(), bson.D{})
	if err != nil {
		return err
	}

	return nil
}

// 根据区域获取集合名称
func getCollectionName(zone string) string {
	// 根据实际需求定义集合名称的规则
	// 这里以"nodes_" + 区域名称作为集合名称的示例
	return "nodes_" + zone
}
