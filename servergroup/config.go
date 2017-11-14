package servergroup

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
)

type Config struct {
	Scheme string                        `yaml:"scheme"`
	Labels model.LabelSet                `json:"labels"`
	Hosts  config.ServiceDiscoveryConfig `yaml:",inline"`
}

func (c *Config) GetScheme() string {
	if c.Scheme == "" {
		return "http"
	} else {
		return c.Scheme
	}
}
