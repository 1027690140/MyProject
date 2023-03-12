package model

import (
	"flag"
	"io/ioutil"
	"os"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/imdario/mergo"

	"service_discovery/pkg/httputil"
)

var (
	confPath      string
	schedulerPath string
	region        string
	zone          string
	deployEnv     string
	hostname      string
	// Conf conf
	Conf = &Config{}
)

func init() {
	var err error
	if hostname, err = os.Hostname(); err != nil || hostname == "" {
		hostname = os.Getenv("HOSTNAME")
	}
	flag.StringVar(&confPath, "conf", "discovery-example.toml", "config path")
	flag.StringVar(&region, "region", os.Getenv("REGION"), "avaliable region. or use REGION env variable, value: sh etc.")
	flag.StringVar(&zone, "zone", os.Getenv("ZONE"), "avaliable zone. or use ZONE env variable, value: sh001/sh002 etc.")
	flag.StringVar(&deployEnv, "deploy.env", os.Getenv("DEPLOY_ENV"), "deploy env. or use DEPLOY_ENV env variable, value: dev/fat1/uat/pre/prod etc.")
	flag.StringVar(&hostname, "hostname", hostname, "machine hostname")
	flag.StringVar(&schedulerPath, "scheduler", "scheduler.json", "scheduler info")
}

//go:generate easytags $GOFILE json
type Config struct {
	Nodes []string            `json:"nodes"`
	Zones map[string][]string `json:"zones"`

	HTTPServer *ServerConfig          `json:"http_server"`
	HTTPClient *httputil.ClientConfig `json:"http_client"`
	Env        *Env                   `json:"env"`

	Scheduler  []byte                `json:"scheduler"`
	Schedulers map[string]*Scheduler `json:"schedulers"`
}

type DiscoveryConfig = Config

// Env is disocvery env.
//
//go:generate easytags $GOFILE json
type Env struct {
	Region    string `json:"region"`
	Zone      string `json:"zone"`
	Host      string `json:"host"`
	DeployEnv string `json:"deploy_env"`
}

// ServerConfig Http Servers conf.
//
//go:generate easytags $GOFILE json
type ServerConfig struct {
	Addr string `json:"addr"`
}

// Default new a config with specified defualt value.
func Default() *Config {
	zone := make(map[string][]string, 1)
	zone["sh003"] = []string{"127.0.0.1:7171"}
	zone["sh004"] = []string{"127.0.0.1:7171"}
	dst := make(map[string]int, 1)
	dst["sz01"] = 3

	schedulers := make(map[string]*Scheduler, 1)
	schedulers["discovery-dev"] = &Scheduler{
		AppID:  "discovery",
		Env:    "dev",
		Remark: "test",
		Zones: []Zone{
			{
				Src: "gd",
				Dst: dst,
			},
		},
	}

	scheduler := []byte(`[{"app_id":"test.service1","env":"uat","zones":[{"src":"sh001","dst":{"sh0001":3,"sh0002":1}},{"src":"sh002","dst":{"hs0001":1,"sh0002":3,"sh0003":4}}],"remark":"te22st"},{"app_id":"test.service2","env":"uat","zones":[{"src":"sh001","dst":{"sh0001":3,"sh0002":1}},{"src":"sh002","dst":{"hs0001":1,"sh0002":3,"sh0003":4}}],"remark":"te22st"}]`)

	cfg := &Config{
		Nodes: []string{"127.0.0.1:7171"},
		HTTPServer: &ServerConfig{
			Addr: "127.0.0.1:7171",
		},
		Zones: zone,

		HTTPClient: &httputil.ClientConfig{
			Dial:      time.Duration(3 * time.Second),
			KeepAlive: time.Duration(120 * time.Second),
		},
		Env: &Env{
			Region:    "china",
			Zone:      "gd",
			DeployEnv: "dev",
			Host:      "discovery",
		},
		Schedulers: schedulers,
		Scheduler:  scheduler,
	}

	return cfg
}

func init() {
	var err error
	if hostname, err = os.Hostname(); err != nil || hostname == "" {
		hostname = os.Getenv("HOSTNAME")
	}
	flag.StringVar(&confPath, "conf", "discovery-example.toml", "config path")
	flag.StringVar(&region, "region", os.Getenv("REGION"), "avaliable region. or use REGION env variable, value: sh etc.")
	flag.StringVar(&zone, "zone", os.Getenv("ZONE"), "avaliable zone. or use ZONE env variable, value: sh001/sh002 etc.")
	flag.StringVar(&deployEnv, "deploy.env", os.Getenv("DEPLOY_ENV"), "deploy env. or use DEPLOY_ENV env variable, value: dev/fat1/uat/pre/prod etc.")
	flag.StringVar(&hostname, "hostname", hostname, "machine hostname")
	flag.StringVar(&schedulerPath, "scheduler", "scheduler.json", "scheduler info")
}

// Fix fix env config.
func (c *Config) Fix() (err error) {
	if c.Env == nil {
		c.Env = new(Env)
	}
	if c.Env.Region == "" {
		c.Env.Region = region
	}
	if c.Env.Zone == "" {
		c.Env.Zone = zone
	}
	if c.Env.Host == "" {
		c.Env.Host = hostname
	}
	if c.Env.DeployEnv == "" {
		c.Env.DeployEnv = deployEnv
	}
	return
}

func Merge(dest, src interface{}) error {
	return mergo.Merge(dest, src)
}

// Init init conf
func Init() (err error) {
	if _, err = toml.DecodeFile(confPath, &Conf); err != nil {
		return
	}
	if schedulerPath != "" {
		Conf.Scheduler, _ = ioutil.ReadFile(schedulerPath)
	}
	return Conf.Fix()
}

// LoadConfig load config from file toml
func LoadConfig(fh string) (*Config, error) {
	_, err := toml.DecodeFile(fh, &Conf)
	return Conf, err

}
