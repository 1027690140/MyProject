package storage

import (
	"encoding/json"
	"fmt"
	"service_discovery/configs"
	"service_discovery/model"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis"
)

var (
	versionCounter     uint64
	lastestVersion     string
	lastestNodeVersion string // 存node 最新version

	cacheExpireTime   int64
	instanceMap       map[string]string // `instanceMap`存储了所有的实例信息，key为`instances:${hostname}`，value为`Instance`对象
	versionCurMap     sync.Map          //存当前version
	versionHistoryMap sync.Map          // `versionHistoryMap`存储了当前版本对应的上一个版本。 如versionMap[curVerison] = preVerison
)

// 变更记录
type RegistryChange struct {
	Action     string                 `json:"action"`
	Timestamp  int64                  `json:"timestamp"`
	RegistryID string                 `json:"registry_id"`
	Version    string                 `json:"version"`
	PreVersion string                 `json:"pre_version"`
	Hostname   string                 `json:"hostname"`
	Data       map[string]interface{} `json:"data"`
}

func init() {
	cacheExpireTime = redisConfig.CacheExpireTime
	instanceMap = make(map[string]string)
}

func GetInstancesByAppID(appID string) ([]*model.Instance, error) {
	instances := []*model.Instance{}

	keys := redisClient.Keys("instances:*").Val()
	for _, key := range keys {
		instanceMap := redisClient.HGetAll(key).Val()

		if instanceMap["app_id"] == appID {
			status, err := strconv.ParseUint(instanceMap["status"], 10, 32)
			if err != nil {
				return nil, fmt.Errorf("failed to parse status: %v", err)
			}

			regTimestamp, err := strconv.ParseInt(instanceMap["reg_timestamp"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse reg_timestamp: %v", err)
			}

			upTimestamp, err := strconv.ParseInt(instanceMap["up_timestamp"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse up_timestamp: %v", err)
			}

			renewTimestamp, err := strconv.ParseInt(instanceMap["renew_timestamp"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse renew_timestamp: %v", err)
			}

			dirtyTimestamp, err := strconv.ParseInt(instanceMap["dirty_timestamp"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse dirty_timestamp: %v", err)
			}

			latestTimestamp, err := strconv.ParseInt(instanceMap["latest_timestamp"], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("failed to parse latest_timestamp: %v", err)
			}

			instance := &model.Instance{
				AppID:           instanceMap["app_id"],
				Env:             instanceMap["env"],
				Zone:            instanceMap["zone"],
				Region:          instanceMap["region"],
				Hostname:        instanceMap["hostname"],
				Version:         instanceMap["version"],
				Status:          uint32(status),
				RegTimestamp:    regTimestamp,
				UpTimestamp:     upTimestamp,
				RenewTimestamp:  renewTimestamp,
				DirtyTimestamp:  dirtyTimestamp,
				LatestTimestamp: latestTimestamp,
			}

			labels := redisClient.SMembers("labels:" + instance.Hostname).Val()
			instance.Labels = labels

			instances = append(instances, instance)
			sort.SliceStable(instances, func(i, j int) bool {
				return instances[i].LatestTimestamp > instances[j].LatestTimestamp
			})
		}
	}

	return instances, nil
}

func GenerateVersion() string {
	version := time.Now().UnixNano()
	versionString := fmt.Sprintf("v%d", version)
	return versionString
}

func updateVersionHistoryMap(currentVersion, previousVersion string) {
	versionHistoryMap.Store(currentVersion, previousVersion)
}

func RollbackToVersion(hostname, version string) error {
	// 从versionCurMap中获取当前版本
	previousVersion, ok := versionCurMap.Load(hostname)
	if !ok {
		return fmt.Errorf("no previous version found")
	}

	instanceMap := redisClient.HGetAll("instances:" + hostname).Val()
	labels := redisClient.SMembers("labels:" + hostname).Val()
	status, _ := strconv.ParseUint(redisClient.HGet("statuses", hostname).Val(), 10, 32)

	var instance model.Instance
	instance.AppID = instanceMap["app_id"]
	instance.Env = instanceMap["env"]
	instance.Zone = instanceMap["zone"]
	instance.Region = instanceMap["region"]
	instance.Hostname = instanceMap["hostname"]
	instance.Version = previousVersion.(string)
	instance.Labels = labels
	instance.Status = uint32(status)
	instance.RegTimestamp, _ = strconv.ParseInt(instanceMap["reg_timestamp"], 10, 64)
	instance.UpTimestamp, _ = strconv.ParseInt(instanceMap["up_timestamp"], 10, 64)
	instance.RenewTimestamp, _ = strconv.ParseInt(instanceMap["renew_timestamp"], 10, 64)
	instance.DirtyTimestamp, _ = strconv.ParseInt(instanceMap["dirty_timestamp"], 10, 64)

	// Store detailed information and version
	redisClient.HMSet("instances:"+hostname, map[string]interface{}{
		"env":              instance.Env,
		"app_id":           instance.AppID,
		"hostname":         instance.Hostname,
		"version":          instance.Version,
		"zone":             instance.Zone,
		"region":           instance.Region,
		"status":           instance.Status,
		"reg_timestamp":    instance.RegTimestamp,
		"up_timestamp":     instance.UpTimestamp,
		"renew_timestamp":  instance.RenewTimestamp,
		"dirty_timestamp":  instance.DirtyTimestamp,
		"latest_timestamp": instance.LatestTimestamp,
	})

	// Store labels
	for _, label := range instance.Labels {
		redisClient.SAdd("labels:"+instance.Hostname, label)
	}

	// Store service status
	redisClient.HSet("statuses", instance.Hostname, instance.Status)

	return nil
}

// RollbackToPreviousVersion rolls back to previous version  回滚到历史版本
func RollbackToPreviousVersion(hostname string) error {
	// Get the current version from versionHistoryMap
	currentVersion, ok := versionHistoryMap.Load(hostname)
	if !ok {
		return fmt.Errorf("no version history found for hostname")
	}

	// Get the previous version from the version history
	previousVersion, err := getPreviousVersion(hostname, currentVersion.(string))
	if err != nil {
		return fmt.Errorf("failed to get previous version: %v", err)
	}

	// Rollback to the previous version
	err = RollbackToVersion(hostname, previousVersion)
	if err != nil {
		return fmt.Errorf("failed to rollback to previous version: %v", err)
	}

	return nil
}

func ClearVersionHistory(hostname string) {
	// Clear version history for the hostname
	versionHistoryMap.Delete(hostname)

}

func StoreVersionHistory(hostname, currentVersion, previousVersion string) {
	versionHistoryMap.Store(currentVersion, previousVersion)
}

func StoreCurVersion(hostname, currentVersion string) {
	versionCurMap.Store(hostname, currentVersion)
}

// UpdateCachedVersion 更新缓存的版本
func UpdateCachedVersion(hostname, version string) {
	instanceMap := redisClient.HGetAll("instances:" + hostname + ":" + version).Val()

	// Convert instanceMap to map[string]interface{}
	instanceMapInterface := make(map[string]interface{})
	for k, v := range instanceMap {
		instanceMapInterface[k] = v
	}

	// Update the version
	instanceMapInterface["version"] = version

	// Update the cached version
	redisClient.HMSet("instances:"+hostname+":"+version, instanceMapInterface)
}

// SaveInstanceToRedis，从versionHistoryMap中获取上一个版本，并在存储实例时将其保存为当前版本的历史记录。
func SaveInstanceToRedis(instance model.Instance) {
	// Generate version
	version := GenerateVersion()
	lastestVersion = version

	// Get previous version from versionHistoryMap
	previousVersion, ok := versionHistoryMap.Load(instance.Hostname)
	if !ok {
		previousVersion = ""
	}

	// Store detailed information and version
	redisClient.HMSet("instances:"+instance.Hostname+":"+version, map[string]interface{}{
		"env":              instance.Env,
		"app_id":           instance.AppID,
		"hostname":         instance.Hostname,
		"version":          version,
		"zone":             instance.Zone,
		"region":           instance.Region,
		"status":           instance.Status,
		"reg_timestamp":    instance.RegTimestamp,
		"up_timestamp":     instance.UpTimestamp,
		"renew_timestamp":  instance.RenewTimestamp,
		"dirty_timestamp":  instance.DirtyTimestamp,
		"latest_timestamp": instance.LatestTimestamp,
	})

	// Store labels
	for _, label := range instance.Labels {
		redisClient.SAdd("labels:"+instance.Hostname+":"+version, label)
	}

	// Store service status 存储服务状态
	redisClient.HSet("statuses", instance.Hostname, instance.Status)

	// Store version history 存储版本历史记录
	StoreVersionHistory(instance.Hostname, version, previousVersion.(string))

	// Store version 存储当前版本
	StoreCurVersion(instance.Hostname, version)

	// Update cached version 更新缓存的版本
	UpdateCachedVersion(instance.Hostname, version)
}

// GetInstanceFromCache，获取实例时从缓存中获取正确的版本号。
func GetInstanceFromCache(hostname string) (model.Instance, error) {
	// 获取当前版本号和键名
	currentVersion := getLatestVersion(hostname)
	key := "instances:" + hostname + ":" + currentVersion

	// 如果当前版本号为空（可能是重启），则从redis获取最新版本号
	if currentVersion == "" {
		instanceKeys := redisClient.Keys("instances:" + hostname + ":*").Val()
		if len(instanceKeys) == 0 {
			return model.Instance{}, fmt.Errorf("instance not found")
		}

		sort.Strings(instanceKeys) // 按照键名排序，确保最新版本在最后

		key = instanceKeys[len(instanceKeys)-1] // 获取最新版本的键名

	}

	// 使用当前版本号和键名获取实例详细信息
	instanceMap := redisClient.HGetAll(key).Val()
	if len(instanceMap) == 0 {
		return model.Instance{}, fmt.Errorf("instance not found")
	}

	labels := redisClient.SMembers("labels:" + hostname).Val()
	status, _ := strconv.ParseUint(redisClient.HGet("statuses", hostname).Val(), 10, 32)

	var instance model.Instance
	instance.AppID = instanceMap["app_id"]
	instance.Env = instanceMap["env"]
	instance.Zone = instanceMap["zone"]
	instance.Region = instanceMap["region"]
	instance.Hostname = instanceMap["hostname"]
	instance.Version = currentVersion
	instance.Labels = labels
	instance.Status = uint32(status)
	instance.RegTimestamp, _ = strconv.ParseInt(instanceMap["reg_timestamp"], 10, 64)
	instance.UpTimestamp, _ = strconv.ParseInt(instanceMap["up_timestamp"], 10, 64)
	instance.RenewTimestamp, _ = strconv.ParseInt(instanceMap["renew_timestamp"], 10, 64)
	instance.DirtyTimestamp, _ = strconv.ParseInt(instanceMap["dirty_timestamp"], 10, 64)
	instance.LatestTimestamp, _ = strconv.ParseInt(instanceMap["latest_timestamp"], 10, 64)

	// 检查实例状态，如果不可用，则获取上一个版本的实例信息
	if instance.Status != configs.StatusOK {
		// 获取上一个版本的实例信息
		previousVersion, err := getPreviousVersion(hostname, currentVersion)
		if err != nil {
			return model.Instance{}, fmt.Errorf("instance not found")
		}
		if previousVersion != "" {
			previousKey := "instances:" + hostname + ":" + previousVersion

			// 使用上一个版本的版本号和键名重新获取实例详细信息
			previousInstanceMap := redisClient.HGetAll(previousKey).Val()
			if len(previousInstanceMap) > 0 {
				instance.AppID = previousInstanceMap["app_id"]
				instance.Env = previousInstanceMap["env"]
				instance.Zone = previousInstanceMap["zone"]
				instance.Region = previousInstanceMap["region"]
				instance.Hostname = previousInstanceMap["hostname"]
				instance.Version = previousVersion
				instance.RegTimestamp, _ = strconv.ParseInt(previousInstanceMap["reg_timestamp"], 10, 64)
				instance.UpTimestamp, _ = strconv.ParseInt(previousInstanceMap["up_timestamp"], 10, 64)
				instance.RenewTimestamp, _ = strconv.ParseInt(previousInstanceMap["renew_timestamp"], 10, 64)
				instance.DirtyTimestamp, _ = strconv.ParseInt(previousInstanceMap["dirty_timestamp"], 10, 64)
				instance.LatestTimestamp, _ = strconv.ParseInt(previousInstanceMap["latest_timestamp"], 10, 64)

				// 更新当前版本号和键名为上一个版本的值
				currentVersion = previousVersion
				key = previousKey
			}
		} else {
			// 去数据库取
			return model.Instance{}, fmt.Errorf("instance not found")
		}

	}

	return instance, nil
}

// 辅助函数：获取上一个版本的实例版本号（状态为可用）
func getPreviousVersion(hostname string, currentVersion string) (string, error) {
	// Get the version history for the hostname
	versionHistory, ok := versionHistoryMap.Load(hostname)
	if !ok {
		return "", fmt.Errorf("no version history found for hostname")
	}

	// Convert the version history to a slice of strings
	versions, ok := versionHistory.([]string)
	if !ok {
		return "", fmt.Errorf("invalid version history format")
	}

	// Find the index of the current version
	currentIndex := -1
	for i, version := range versions {
		if version == currentVersion {
			currentIndex = i
			break
		}
	}

	// If the current version was not found or it is the first version, return an error
	if currentIndex == -1 || currentIndex == 0 {
		return "", fmt.Errorf("no previous version found")
	}

	// Get the previous version
	previousVersion := versions[currentIndex-1]

	return previousVersion, nil
}

// 获取最新版本
func getLatestVersion(hostname string) string {
	return lastestVersion
}

// CleanupExpiredCache 清理过期的缓存和状态不可用的缓存
func CleanupExpiredCache(cacheExpireTime int64) {
	now := time.Now().Unix()

	// Iterate over all hostnames in version history map
	versionHistoryMap.Range(func(key, value interface{}) bool {
		hostname := key.(string)
		latestVersion := value.(string)

		// Get the latest timestamp and status of the instance
		redisKey := fmt.Sprintf("instances:%s:%s", hostname, latestVersion)
		timestamp, err := redisClient.HGet(redisKey, "latest_timestamp").Int64()
		if err != nil {
			// Failed to retrieve the timestamp, remove the entry from version history map
			versionHistoryMap.Delete(hostname)
			return true
		}

		status, err := redisClient.HGet(redisKey, "status").Uint64()
		if err != nil {
			// Failed to retrieve the status, remove the entry from version history map
			versionHistoryMap.Delete(hostname)
			return true
		}

		// Check if the instance has expired or if the status is not available
		if now-timestamp > cacheExpireTime || status != configs.StatusOK {
			// Remove the entry from Redis and version history map
			redisClient.Del(redisKey)
			versionHistoryMap.Delete(hostname)
		}
		// Check if the version is expired or the instance has a status not equal to config.StatusOK
		if isExpired(latestVersion) || !isStatusOK(hostname) {
			redisClient.Del(hostname)
			redisClient.Del("instances:" + hostname)
			redisClient.Del("labels:" + hostname)
			redisClient.HDel("statuses", hostname)
			versionCurMap.Delete(hostname)
			versionHistoryMap.Delete(hostname)
		}

		return true
	})

	// 删除同时持久化到mongo
	// go storeInstance()
}

// RecordRegistryChange 记录注册中心变更记录
func RecordRegistryChange(action, registryID, version, preVersion, hostname string, data map[string]interface{}) error {
	// 创建变更记录对象
	change := RegistryChange{
		Action:     action,
		Timestamp:  time.Now().Unix(),
		RegistryID: registryID,
		Version:    version,
		PreVersion: preVersion,
		Hostname:   hostname,
		Data:       data,
	}

	// 序列化变更记录对象为JSON字符串
	changeJSON, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("failed to marshal registry change: %v", err)
	}

	// 将变更记录添加到Redis列表中
	redisKey := "registry_changes"
	err = redisClient.LPush(redisKey, changeJSON).Err()
	if err != nil {
		return fmt.Errorf("failed to record registry change in Redis: %v", err)
	}

	return nil
}

// GetAllRegistryChanges 获取所有的变更记录
func GetAllRegistryChanges() ([]RegistryChange, error) {
	// 获取所有的变更记录
	redisKey := "registry_changes"
	changeJSONs, err := redisClient.LRange(redisKey, 0, -1).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve registry changes from Redis: %v", err)
	}

	// 反序列化变更记录JSON字符串为对象
	changes := make([]RegistryChange, len(changeJSONs))
	for i, changeJSON := range changeJSONs {
		err = json.Unmarshal([]byte(changeJSON), &changes[i])
		if err != nil {
			return nil, fmt.Errorf("failed to unmarshal registry change: %v", err)
		}
	}

	return changes, nil
}

// GetRegistryChanges 获取指定版本的变更记录
func GetRegistryChanges(version string) ([]RegistryChange, error) {
	// 按版本号从Redis中获取变更记录
	key := fmt.Sprintf("registry_changes:%s", version)
	values, err := redisClient.HGetAll(key).Result()
	if err != nil {
		return nil, err
	}

	// 将获取到的变更记录转换为RegistryChange结构体切片
	changes := make([]RegistryChange, 0, len(values))
	for _, value := range values {
		var change RegistryChange
		err := json.Unmarshal([]byte(value), &change)
		if err != nil {
			return nil, err
		}
		changes = append(changes, change)
	}

	// 按时间戳降序排序变更记录
	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Timestamp > changes[j].Timestamp
	})

	// 只保留最近的5条变更记录
	if len(changes) > 5 {
		changes = changes[:5]
	}

	return changes, nil
}

// AddRegistryChange 添加变更记录
func AddRegistryChange(change RegistryChange) error {
	changeBytes, err := json.Marshal(change)
	if err != nil {
		return fmt.Errorf("failed to marshal registry change: %v", err)
	}

	// 添加到Redis有序集合中
	_, err = redisClient.ZAdd("registry_changes", redis.Z{
		Score:  float64(change.Timestamp),
		Member: changeBytes,
	}).Result()
	if err != nil {
		return fmt.Errorf("failed to add registry change to sorted set: %v", err)
	}

	return nil
}

// GetLatestRegistryChange 获取最新的变更记录
func GetLatestRegistryChange() (RegistryChange, error) {
	var latestChange RegistryChange

	// Get the latest registry change from the Redis sorted set
	change, err := redisClient.ZRevRange("registry_changes", 0, 0).Result()
	if err != nil || len(change) == 0 {
		return latestChange, fmt.Errorf("failed to get latest registry change")
	}

	err = json.Unmarshal([]byte(change[0]), &latestChange)
	if err != nil {
		return latestChange, fmt.Errorf("failed to parse latest registry change: %v", err)
	}

	return latestChange, nil
}

// 存储节点到Redis
// 存储节点到Redis的Hash
func storeNodeToRedis(node model.Node) error {
	if redisClient == nil {
		return fmt.Errorf("Redis client not initialized")
	}

	key := fmt.Sprintf("node:%s", node.Addr)

	// Convert the node struct to a map[string]interface{} for the hash fields
	nodeFields := map[string]interface{}{
		"config":       node.Config,
		"status":       node.Status,
		"register_url": node.RegisterURL,
		"cancel_url":   node.CancelURL,
		"renew_url":    node.RenewURL,
		"poll_url":     node.PollURL,
		"polls_url":    node.PollsURL,
		"zone":         node.Zone,
	}

	err := redisClient.HMSet(key, nodeFields).Err()
	if err != nil {
		return err
	}

	fmt.Println("Node stored successfully")
	return nil
}

// 通过节点地址从Redis的Hash获取节点
func getNodeByAddrInRedis(addr string) (model.Node, error) {
	var node model.Node

	if redisClient == nil {
		return node, fmt.Errorf("Redis client not initialized")
	}

	key := fmt.Sprintf("node:%s", addr)

	result, err := redisClient.HGetAll(key).Result()
	if err != nil {
		return node, err
	}

	if len(result) == 0 {
		return node, fmt.Errorf("Node not found")
	}

	node.RegisterURL = result["register_url"]
	node.CancelURL = result["cancel_url"]
	node.RenewURL = result["renew_url"]
	node.PollURL = result["poll_url"]
	node.PollsURL = result["polls_url"]
	node.Zone = result["zone"]

	return node, nil
}

// SaveClusterNodesToRedis 将注册中心的集群节点信息保存到Redis中
func SaveClusterNodesToRedis(nodes *model.Nodes) error {
	// 将Nodes转换为JSON字符串
	nodesJSON, err := json.Marshal(nodes)
	if err != nil {
		return err
	}

	version := getLatestNodeVersion()
	// 生成缓存键名
	key := "nodes:" + nodes.Zone + ":" + version

	// 存储节点信息到Redis中
	redisClient.HMSet(key, map[string]interface{}{
		"nodes":     string(nodesJSON),
		"zone":      nodes.Zone,
		"self_addr": nodes.SelfAddr,
		"version":   version,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
	})

	return nil
}

func getLatestNodeVersion() string {
	lastestNodeVersion = GenerateVersion()
	return lastestNodeVersion
}

// GetClusterNodesFromCache 从Redis中获取注册中心的集群节点信息
func GetClusterNodesFromCache() (*model.Nodes, error) {
	// 生成缓存键名
	key := "nodes:" + getLatestNodeVersion()

	// 从Redis中获取节点信息
	result, err := redisClient.HGetAll(key).Result()
	if err != nil {
		return nil, err
	}

	// 解析节点信息
	nodesJSON, ok := result["nodes"]
	if !ok {
		return nil, fmt.Errorf("nodes not found in cache")
	}
	var nodes model.Nodes
	err = json.Unmarshal([]byte(nodesJSON), &nodes)
	if err != nil {
		return nil, err
	}

	nodes.Zone = result["zone"]
	nodes.SelfAddr = result["self_addr"]

	return &nodes, nil
}

func isExpired(version string) bool {
	versionTime, err := strconv.ParseInt(version[1:], 10, 64)
	if err != nil {
		return true
	}
	expireTime := time.Now().Unix() - cacheExpireTime
	return versionTime < expireTime
}

func isStatusOK(hostname string) bool {
	status, err := strconv.ParseUint(redisClient.HGet("statuses", hostname).Val(), 10, 32)
	if err != nil {
		return false
	}
	return status == configs.StatusOK
}
