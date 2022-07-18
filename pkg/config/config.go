package proxyconfig

import (
	"fmt"
	"io/ioutil"

	"github.com/prometheus/exporter-toolkit/web"

	"github.com/prometheus/prometheus/config"

	"github.com/jacksontj/promxy/pkg/servergroup"

	"github.com/prometheus/common/sigv4"

	yaml "gopkg.in/yaml.v2"
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
	configBytes, err := ioutil.ReadFile(path)
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

// PromxyConfig is the configuration for Promxy itself
type PromxyConfig struct {
	// Config for each of the server groups promxy is configured to aggregate
	ServerGroups []*servergroup.Config `yaml:"server_groups"`
	SigV4Config  *sigv4.SigV4Config    `yaml:"sigv4,omitempty"`
}
