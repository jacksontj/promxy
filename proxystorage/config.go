package proxystorage

import (
	"fmt"
	"io/ioutil"
	"time"

	"github.com/jacksontj/promxy/servergroup"

	"gopkg.in/yaml.v2"
)

func ConfigFromFile(path string) (*Config, error) {
	// load the config file
	config := &Config{}
	configBytes, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("Error loading config: %v", err)
	}
	err = yaml.Unmarshal([]byte(configBytes), &config)
	if err != nil {
		return nil, fmt.Errorf("Error unmarshaling config: %v", err)
	}

	// TODO: better -- config needs to be split out into different groups
	for _, s := range config.ServerGroups {
		s.AutoRefresh(config.ServerGroupRefreshInterval)
	}

	return config, nil
}

// Common configuration for all storage nodes
type Config struct {
	ServerGroups               []*servergroup.ServerGroup `yaml:"server_groups"`
	ServerGroupRefreshInterval time.Duration              `yaml:"server_group_refresh_interval"`
}
