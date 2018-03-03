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
	cfg := &Config{
		PromConfig: config.DefaultConfig,
	}
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
	// Prometheus configs -- this includes configurations for
	// recording rules, alerting rules, etc.
	PromConfig config.Config `yaml:",inline"`

	// Promxy specific configuration -- under its own namespace
	PromxyConfig `yaml:"promxy"`
}

type PromxyConfig struct {
	// HTTP client config for promxy to use when connecting to the various server_groups
	// this is the same config as prometheus
	HTTPConfig config.HTTPClientConfig `yaml:"http_client"`
	// Config for each fo the server groups
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
}
