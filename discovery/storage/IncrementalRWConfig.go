package storage

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"path/filepath"
	"service_discovery/model"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// 配置文件状态常量
const (
	FileStatusUnchanged = "unchanged"
	FileStatusModified  = "modified"
	FileStatusDeleted   = "deleted"
	FileStatusChanged   = "changed"
)

// ConfigFile represents a configuration file.
type ConfigFile struct {
	ID         string         `bson:"id" json:"id"`
	Status     string         `bson:"status" json:"status"`
	Checksum   string         `bson:"checksum" json:"checksum"`
	Content    string         `bson:"content" json:"content"`
	FileName   string         `bson:"file_name" json:"file_name"`
	LastUpdate time.Time      `bson:"last_update" json:"last_update"`
	Instance   model.Instance `bson:"instance" json:"instance"`
	Nodes      model.Nodes    `bson:"nodes" json:"nodes"`
}

type MyRegistry struct {
	Registry *model.Registry
}

// VersionMessage represents the version information and configuration files.
type VersionMessage struct {
	Version string       `bson:"version" json:"version"`
	Files   []ConfigFile `bson:"files" json:"files"`
}

// 全局哈希表，用于存储文件ID与对应的配置文件
var configFileIndex map[string]*ConfigFile

// 初始化全局哈希表
func initConfigFileIndex(files []ConfigFile) {
	configFileIndex = make(map[string]*ConfigFile)
	for i := range files {
		configFileIndex[files[i].ID] = &files[i]
	}
}

// 根据ID查找配置文件
func findConfigFileByID(id string) *ConfigFile {
	return configFileIndex[id]
}

func readLocalFiles() ([]ConfigFile, error) {
	configDir := "../cmd"

	files, err := ioutil.ReadDir(configDir)
	if err != nil {
		return nil, err
	}

	var configFiles []ConfigFile
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filePath := filepath.Join(configDir, file.Name())
		content, err := ioutil.ReadFile(filePath)
		if err != nil {
			return nil, err
		}

		configFile := ConfigFile{
			ID:         generateFileID(file.Name()),
			Status:     FileStatusUnchanged,
			Content:    string(content),
			LastUpdate: time.Now(),
			FileName:   file.Name(),
		}

		configFiles = append(configFiles, configFile)
	}

	return configFiles, nil
}

// 生成文件ID
func generateFileID(fileName string) string {
	hash := sha1.New()
	hash.Write([]byte(fileName))
	hashBytes := hash.Sum(nil)
	return hex.EncodeToString(hashBytes)
}

func readRedisFiles() ([]ConfigFile, error) {

	// 从Redis读取配置文件
	val, err := redisClient.Get("config.yaml").Result()
	if err != nil {
		return nil, err
	}

	// 解码JSON数据
	var files []ConfigFile
	err = json.Unmarshal([]byte(val), &files)
	if err != nil {
		return nil, err
	}

	return files, nil
}

func readVersionMessageFromMongoDB() (VersionMessage, error) {
	// 选择数据库和集合
	database := mongoClient.Database("mydb")
	collection := database.Collection("versions")

	// 查询最新的版本信息
	filter := bson.M{}
	options := options.FindOneOptions{}
	// 按版本号倒序排序，取第一条记录
	options.SetSort(bson.D{{"version", -1}})
	var versionMsg VersionMessage
	err := collection.FindOne(context.Background(), filter, &options).Decode(&versionMsg)
	if err != nil {
		return VersionMessage{}, err
	}

	return versionMsg, nil
}

func checkAndUpdateFiles(files []ConfigFile, versionMsg VersionMessage) []ConfigFile {
	updatedFiles := make([]ConfigFile, 0)

	for _, file := range files {
		// 查找配置文件在版本信息中的对应项
		found := false
		for _, versionFile := range versionMsg.Files {
			if versionFile.ID == file.ID {
				found = true

				// 检查变更状态
				if versionFile.Status == "changed" {
					// 根据Checksum判断是否需要更新
					if versionFile.Checksum != file.Checksum {
						// 更新配置文件
						file.Status = "updated"
						file.LastUpdate = time.Now()
						updatedFiles = append(updatedFiles, file)
						// 更新全局哈希表
						configFileIndex[file.ID] = &file
					}
				}
				break
			}
		}

		// 如果在版本信息中找不到对应项，则将该文件标记为新增
		if !found {
			file.Status = "added"
			file.LastUpdate = time.Now()
			updatedFiles = append(updatedFiles, file)
		}
	}

	return updatedFiles
}

// 合并配置文件并广播
func (r *MyRegistry) mergeAndBroadcast(env, appid string) {
	// 获取最近一次发布生效的配置
	latestVersionMsg, err := readVersionMessageFromMongoDB()
	if err != nil {
		log.Println("Failed to retrieve latest version message:", err)
		return
	}

	// 获取本地缓存的配置
	localFiles, err := readLocalFiles()
	if err != nil {
		log.Println("Failed to read local files:", err)
		return
	}

	// 比较配置变更状态
	changedFiles := make([]ConfigFile, 0)
	for _, latestFile := range latestVersionMsg.Files {
		localFile := findConfigFileByID(latestFile.ID)
		if localFile == nil || localFile.Checksum != latestFile.Checksum {
			changedFiles = append(changedFiles, latestFile)
		}
	}

	// 合并配置
	mergedFiles := mergeConfigFiles(localFiles, changedFiles)

	// 更新本地缓存
	err = updateCache(mergedFiles)
	if err != nil {
		log.Println("Failed to update local files:", err)
		return
	}

	// 广播配置更新
	r.Registry.Broadcast(env, appid)
}

// 合并文件
func mergeConfigFiles(redisFiles, localFiles []ConfigFile) []ConfigFile {
	mergedFiles := make([]ConfigFile, 0)

	// 初始化或更新全局哈希表
	if configFileIndex == nil {
		initConfigFileIndex(redisFiles)
	} else {
		for _, file := range redisFiles {
			configFileIndex[file.ID] = &file
		}
	}

	// 添加Redis文件到合并结果，进行去重
	for _, file := range redisFiles {
		if _, ok := configFileIndex[file.ID]; !ok {
			configFileIndex[file.ID] = &file
		}
	}

	// 添加本地文件到合并结果，进行去重
	for _, file := range localFiles {
		if _, ok := configFileIndex[file.ID]; !ok {
			configFileIndex[file.ID] = &file
		}
	}

	// 将去重后的文件加入合并结果
	for _, file := range configFileIndex {
		mergedFiles = append(mergedFiles, *file)
	}

	return mergedFiles
}

// 更新缓存
func updateCache(files []ConfigFile) error {

	// 更新 Redis 缓存
	err := updateRedisCache(files)
	if err != nil {
		return err
	}

	// 更新 MongoDB 缓存
	err = updateMongoDBCache(files)
	if err != nil {
		return err
	}

	// 从 Redis 缓存中删除文件
	err = deleteFromRedisCache(files)
	if err != nil {
		return err
	}

	// 从 MongoDB 缓存中删除文件
	err = deleteFromMongoDBCache(files)
	if err != nil {
		return err
	}

	return nil
}

// 将更新后的配置写入本地文件
func writeLocalFiles(files []ConfigFile) error {
	for _, file := range files {
		// 构建文件路径
		filePath := fmt.Sprintf("/path/files/%s", file.FileName)

		// 将配置内容写入文件
		err := ioutil.WriteFile(filePath, []byte(file.Content), 0644)
		if err != nil {
			return fmt.Errorf("failed to write file %s: %w", filePath, err)
		}
	}

	return nil
}

// 更新Redis缓存
func updateRedisCache(files []ConfigFile) error {
	for _, file := range files {
		// 根据文件状态执行相应的操作
		switch file.Status {
		case FileStatusModified:
			// 更新Redis缓存
			SaveInstanceToRedis(file.Instance)

		case FileStatusDeleted:
			// 删除Redis缓存中的相应文件
			err := DeleteInstanceFromRedis(file)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// 更新MongoDB缓存
func updateMongoDBCache(files []ConfigFile) error {
	for _, file := range files {
		// 根据文件状态执行相应的操作
		switch file.Status {
		case FileStatusChanged:
		case FileStatusModified:
			// 更新MongoDB缓存
			err := storeInstance(file.Instance)
			if err != nil {
				return err
			}
		case FileStatusDeleted:
			// 从MongoDB缓存中删除相应文件
			err := deleteInstanceFromMongoDB(file)
			if err != nil {
				return err
			}

		}
	}

	return nil
}

// 从 MongoDB 缓存中删除文件
func deleteInstanceFromMongoDB(file ConfigFile) error {
	// 获取数据库和集合句柄
	db := mongoClient.Database(databaseName)
	collection := db.Collection(collectionName)

	// 根据文件ID删除文件
	filter := bson.M{"id": file.ID}
	_, err := collection.DeleteOne(context.TODO(), filter)
	if err != nil {
		return err
	}
	delete(configFileIndex, file.ID)

	return nil
}

// 从 Redis 缓存中删除文件
func DeleteInstanceFromRedis(file ConfigFile) error {
	// 删除 Redis 中的文件数据
	err := redisClient.Del(file.ID).Err()
	if err != nil {
		fmt.Errorf("failed to delete file %s from Redis: %w", file.FileName, err)
		return err
	}
	delete(configFileIndex, file.ID)

	return nil
}

// 从 MongoDB 缓存中删除文件
func deleteFromMongoDBCache(files []ConfigFile) error {
	// 获取数据库和集合句柄
	db := mongoClient.Database(databaseName)
	collection := db.Collection(collectionName)

	for _, file := range files {
		// 根据文件ID删除文件
		filter := bson.M{"id": file.ID}
		_, err := collection.DeleteOne(context.TODO(), filter)
		if err != nil {
			fmt.Errorf("failed to delete file %s from MongoDB: %w", file.FileName, err)
			return err
		}
		delete(configFileIndex, file.ID)
	}

	return nil
}

// 从 Redis 缓存中删除文件
func deleteFromRedisCache(files []ConfigFile) error {
	for _, file := range files {
		// 删除 Redis 中的文件数据
		err := redisClient.Del(file.ID).Err()
		if err != nil {
			fmt.Errorf("failed to delete file %s from Redis: %w", file.FileName, err)
			return err
		}
		delete(configFileIndex, file.ID)

	}
	return nil
}
