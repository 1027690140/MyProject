package configs

type GlobalConfig struct {
	Nodes      []string `yaml:"nodes"`
	Zone       string   `yaml:"zones"`
	Hostname   string   `yaml:"hostname"`
	Env        string   `yaml:"env"`
	HttpServer string   `yaml:"http_server"`
	Protect    bool     `yaml:"protect"`
}
