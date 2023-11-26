package config

import (
	"encoding/json"
	"io/ioutil"
	"path/filepath"
)

type MongoConfig struct {
	URI                string `json:"uri"`
	Database           string `json:"database"`
	Collection         string `json:"collection"`
	InstanceCollection string `json:"instance_collection"`
}

func LoadMongoConfig() (MongoConfig, error) {
	configFile := "mongoConfig.yaml" // Specify the path to your MongoDB configuration file

	absPath, err := filepath.Abs(configFile)
	if err != nil {
		return MongoConfig{}, err
	}

	fileBytes, err := ioutil.ReadFile(absPath)
	if err != nil {
		return MongoConfig{}, err
	}

	var config MongoConfig
	err = json.Unmarshal(fileBytes, &config)
	if err != nil {
		return MongoConfig{}, err
	}

	return config, nil
}
