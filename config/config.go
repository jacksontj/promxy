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

// Config is the entire config file. This includes both the Prometheus Config
// as well as the Promxy config. This is done by "inline-ing" the promxy
// config into the prometheus config under the "promxy" key
type Config struct {
	// Prometheus configs -- this includes configurations for
	// recording rules, alerting rules, etc.
	PromConfig config.Config `yaml:",inline"`

	// Promxy specific configuration -- under its own namespace
	PromxyConfig `yaml:"promxy"`
}

// PromxyConfig is the configuration for Promxy itself
type PromxyConfig struct {
	// Config for each of the server groups promxy is configured to aggregate
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
}
