package servergroup

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
)

type Config struct {
	// Scheme defines how promxy talks to this server group (http, https, etc.)
	Scheme string `yaml:"scheme"`
	// Labels is a set of labels that will be added to all metrics retrieved
	// from this server group
	Labels model.LabelSet `json:"labels"`
	// RelabelConfigs is identical in function and configuration as prometheus'
	// relabel config for scrape jobs
	RelabelConfigs []*config.RelabelConfig `yaml:"relabel_configs,omitempty"`
	// Hosts is a set of ServiceDiscoveryConfig options that allow promxy to discover
	// all hosts in the server_group
	Hosts config.ServiceDiscoveryConfig `yaml:",inline"`
	// TODO cache this as a model.Time after unmarshal
	// AntiAffinity defines how large of a gap in the timeseries will cause promxy
	// to merge series from 2 hosts in a server_group. This required for a couple reasons
	// (1) Promxy cannot make assumptions on downstream clock-drift and
	// (2) two prometheus hosts scraping the same target may have different times
	// #2 is caused by prometheus storing the time of the scrape as the time the scrape **starts**.
	// in practice this is actually quite frequent as there are a variety of situations that
	// cause variable scrape completion time (slow exporter, serial exporter, network latency, etc.)
	// any one of these can cause the resulting data in prometheus to have the same time but in reality
	// come from different points in time. Best practice for this value is to set it to your scrape interval
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
