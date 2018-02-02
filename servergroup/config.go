package servergroup

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
)

type Config struct {
	Scheme string         `yaml:"scheme"`
	Labels model.LabelSet `json:"labels"`
	// List of target relabel configurations.
	RelabelConfigs []*config.RelabelConfig       `yaml:"relabel_configs,omitempty"`
	Hosts          config.ServiceDiscoveryConfig `yaml:",inline"`
	// TODO cache this as a model.Time after unmarshal
	AntiAffinity *time.Duration `yaml:"anti_affinity,omitempty"`
}

func (c *Config) GetScheme() string {
	if c.Scheme == "" {
		return "http"
	} else {
		return c.Scheme
	}
}

func (c *Config) GetAntiAffinity() model.Time {
	if c.AntiAffinity == nil {
		return model.TimeFromUnix(10) // 10s
	} else {
		return model.TimeFromUnix(int64((*c.AntiAffinity).Seconds()))
	}
}
