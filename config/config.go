package proxyconfig

import (
	"fmt"
	"io/ioutil"
	"time"

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
	PromConfig config.Config `yaml:",inline"`

	PromxyConfig `yaml:"promxy"`
}

type PromxyConfig struct {
	HTTPConfig config.HTTPClientConfig `yaml:"http_client"`
	// Our own configs
	ServerGroups []*servergroup.Config `yaml:"server_groups"`

	ProxyQuerierConfig ProxyQuerierConfig `yaml:"proxy_querier"`
}

type ProxyQuerierConfig struct {
	// Configs to control downsampling on QueryRange
	QueryRangeConfig *ProxyQuerierQueryRangeConfig `yaml:"query_range"`
}

// TODO: make per-serverGroup configurable?
type ProxyQuerierQueryRangeConfig struct {
	MaxDatapoints  int           `yaml:"max_datapoints"`
	ScrapeInterval time.Duration `yaml:"scrape_interval"`
}
