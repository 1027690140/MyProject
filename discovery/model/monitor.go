package model

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"service_discovery/configs"
	"service_discovery/pkg/queue"
	"sync/atomic"
	"time"

	"gopkg.in/yaml.v2"
)

// monitorTheConfiguration monitor the configuration file for changes 动态监听配置文件变化
func MonitorTheConfiguration(cig string, dis *Discovery) {
	lastModified := time.Now()

	for {
		// Check the modification time of the configuration file
		fileInfo, err := os.Stat(cig)
		if err != nil {
			fmt.Println("Failed to get file info:", err)
		} else {
			modifiedTime := fileInfo.ModTime()

			// If the file has been modified, reload the configuration
			if modifiedTime.After(lastModified) {
				lastModified = modifiedTime

				newConfig, err := loadConfig(cig)
				if err != nil {
					log.Println("Failed to load updated configuration:", err)
				} else {
					// Apply the updated configuration
					err = dis.updateConfig(newConfig)
					if err != nil {
						log.Println("Failed to apply updated configuration:", err)
						// 开启重试机制
						// 加入失败队列
						if atomic.LoadInt32(&gobalfailmonitorList.size) == 0 { //队列为空
							q := queue.NewQueue()
							q.Add(cig)
							q.Add(dis)
							gobalfailList.list.Add(q)
							atomic.AddInt32(&gobalfailmonitorList.size, 1)
						} else {
							p := gobalfailList.list.Peek().(*queue.Queue)
							p.Add(cig)
							p.Add(dis)
						}
						//异步执行
						go monitorRetry(gobalfailList)

					} else {
						log.Println("Successfully applied updated configuration")
					}

				}
			}
		}

		time.Sleep(time.Second) // Sleep for a certain interval before checking again
	}
}

func loadConfig(c string) (*configs.GlobalConfig, error) {
	configFile, err := ioutil.ReadFile(c)
	if err != nil {
		return nil, err
	}
	config := new(configs.GlobalConfig)
	err = yaml.Unmarshal(configFile, config)
	if err != nil {
		return nil, err
	}
	return config, nil
}

func (dis *Discovery) updateConfig(config *configs.GlobalConfig) error {
	now := time.Now().UnixNano()
	dis.config = config
	instance := &Instance{
		Env:             config.Env,
		Hostname:        config.Hostname,
		AppID:           configs.DiscoveryAppID,
		Addrs:           []string{"http://" + config.HttpServer},
		Status:          configs.NodeStatusUp,
		RegTimestamp:    now,
		UpTimestamp:     now,
		LatestTimestamp: now,
		RenewTimestamp:  now,
		DirtyTimestamp:  now,
	}
	//broadcast
	return dis.Nodes.Load().(*Nodes).Replicate(configs.Renew, instance) //broadcast
}
