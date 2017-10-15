package proxyconfig

import (
	"fmt"
	"io/ioutil"

	"github.com/jacksontj/promxy/servergroup"
	"github.com/prometheus/prometheus/config"

	"gopkg.in/yaml.v2"
)

func ConfigFromFile(path string) (*Config, error) {
	// load the config file
	cfg := &Config{}
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error loading config: %v", err)
	}
	err = yaml.Unmarshal([]byte(configBytes), &cfg)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshaling config: %v", err)
	}

	return cfg, nil
}

// Common configuration for all storage nodes
type Config struct {
	PromConfig config.Config `yaml:",inline"`

	// Our own configs
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
}
