package global

import (
	"io/ioutil"
	"service_discovery/configs"
	"service_discovery/model"

	"gopkg.in/yaml.v2"
)

// init discovery
var Discovery *model.Discovery

func LoadConfig(c string) (*configs.GlobalConfig, error) {
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
