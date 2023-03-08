package proxyconfig

import (
	"fmt"
	"os"

	"github.com/prometheus/exporter-toolkit/web"
	"github.com/prometheus/prometheus/config"
	yaml "gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/servergroup"
)

// DefaultPromxyConfig is the default promxy config that the config file
// is loaded into
var DefaultPromxyConfig = PromxyConfig{}

// ConfigFromFile loads a config file at path
func ConfigFromFile(path string) (*Config, error) {
	// load the config file
	cfg := &Config{
		PromConfig:   config.DefaultConfig,
		PromxyConfig: DefaultPromxyConfig,
	}
	configBytes, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("error loading config: %v", err)
	}
	err = yaml.Unmarshal([]byte(configBytes), &cfg)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling config: %v", err)
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

	WebConfig web.TLSStruct `yaml:"tls_server_config"`
}

func (c *Config) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("<error creating config string: %s>", err)
	}
	return string(b)
}

// PromxyConfig is the configuration for Promxy itself
type PromxyConfig struct {
	// Config for each of the server groups promxy is configured to aggregate
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
}
