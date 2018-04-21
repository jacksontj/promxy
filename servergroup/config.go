package servergroup

import (
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	sd_config "github.com/prometheus/prometheus/discovery/config"
)

// Config is the configuration for a ServerGroup that promxy will talk to.
// This is where the vast majority of options exist.
type Config struct {
	// RemoteRead directs promxy to load data (from the storage API) through the
	// remoteread API on prom.
	// Pros:
	//  - StaleNaNs work
	//  - ~2x faster (in my local testing, more so if you are using default JSON marshaler in prom)
	//
	// Cons:
	//  - proto marshaling prom side doesn't stream, so the data being sent
	//      over the wire will be 2x its size in memory on the remote prom host.
	//  - "experimental" API (according to docs) -- meaning this might break
	//      without much (if any) warning
	//
	// Upstream prom added a StaleNan to determine if a given timeseries has gone
	// NaN -- the problem being that for range vectors they filter out all "stale" samples
	// meaning that it isn't possible to get a "raw" dump of data through the query/query_range v1 API
	// The only option that exists in reality is the "remote read" API -- which suffers
	// from the same memory-balooning problems that the HTTP+JSON API originally had.
	// It has **less** of a problem (its 2x memory instead of 14x) so it is a viable option.
	RemoteRead bool `yaml:"remote_read"`
	// HTTP client config for promxy to use when connecting to the various server_groups
	// this is the same config as prometheus
	HTTPConfig config_util.HTTPClientConfig `yaml:"http_client"`
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
	Hosts sd_config.ServiceDiscoveryConfig `yaml:",inline"`
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
	}
	return c.Scheme
}

func (c *Config) GetAntiAffinity() model.Time {
	if c.AntiAffinity == nil {
		return model.TimeFromUnix(10) // 10s
	}
	return model.TimeFromUnix(int64((*c.AntiAffinity).Seconds()))
}
