package servergroup

import (
	"fmt"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"

	"github.com/jacksontj/promxy/pkg/promclient"

	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/model/relabel"
)

var (
	// DefaultConfig is the Default base promxy configuration
	DefaultConfig = Config{
		AntiAffinity:        time.Second * 10,
		Scheme:              "http",
		RemoteReadPath:      "api/v1/read",
		Timeout:             0,
		MaxIdleConns:        20000,
		MaxIdleConnsPerHost: 1000,
		IdleConnTimeout:     5 * time.Minute,
		PreferMax:           false,
		HTTPConfig: HTTPClientConfig{
			DialTimeout: time.Millisecond * 200, // Default dial timeout of 200ms
		},
	}
)

const (
	// PathPrefixLabel is the name of the label that holds the path prefix for a scrape target.
	PathPrefixLabel = "__path_prefix__"
)

// Config is the configuration for a ServerGroup that promxy will talk to.
// This is where the vast majority of options exist.
type Config struct {
	Ordinal int `yaml:"-"`
	// RemoteRead directs promxy to load RAW data (meaning matrix selectors such as `foo[1h]`)
	// through the RemoteRead API on prom.
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
	// RemoteReadPath sets the remote read path for the hosts in this servergroup
	RemoteReadPath string `yaml:"remote_read_path"`
	// HTTP client config for promxy to use when connecting to the various server_groups
	// this is the same config as prometheus
	HTTPConfig HTTPClientConfig `yaml:"http_client"`
	// Scheme defines how promxy talks to this server group (http, https, etc.)
	Scheme string `yaml:"scheme"`
	// Labels is a set of labels that will be added to all metrics retrieved
	// from this server group
	Labels model.LabelSet `json:"labels"`
	// RelabelConfigs are similar in function and identical in configuration as prometheus'
	// relabel config for scrape jobs. The difference here being that the source labels
	// you can pull from are from the downstream servergroup target and the labels you are
	// relabeling are that of the timeseries being returned. This allows you to mutate the
	// labelsets returned by that target at runtime.
	// To further illustrate the difference we'll look at an example:
	//
	//      relabel_configs:
	//    - source_labels: [__meta_consul_tags]
	//      regex: '.*,prod,.*'
	//      action: keep
	//    - source_labels: [__meta_consul_dc]
	//      regex: '.+'
	//      action: replace
	//      target_label: datacenter
	//
	// If we saw this in a scrape-config we would expect:
	//   (1) the scrape would only target hosts with a prod consul label
	//   (2) it would add a label to all returned series of datacenter with the value set to whatever the value of __meat_consul_dc was.
	//
	// If we saw this same config in promxy (pointing at prometheus hosts instead of some exporter), we'd expect a similar behavior:
	//   (1) only targets with the prod consul label would be included in the servergroup
	//   (2) it would add a label to all returned series of this servergroup of datacenter with the value set to whatever the value of __meat_consul_dc was.
	//
	// So in reality its "the same", the difference is in prometheus these apply to the labels/targets of a scrape job,
	// in promxy they apply to the prometheus hosts in the servergroup - but the behavior is the same.
	RelabelConfigs []*relabel.Config `yaml:"relabel_configs,omitempty"`
	// MetricsRelabelConfigs are similar in spirit to prometheus' relabel config but quite different.
	// As this relabeling is being done within the query-path all relabel actions need to be reversible
	// so that we can alter queries (e.g. matchers) based on the relabel config. This is done by strictly
	// limiting the rewrite capability to just those subset of actions that can be reversed. Similar to
	// prometheus' relabel capability these rules are executed in an order -- so the rules can be compounded
	// to create relatively complex relabel behavior.
	// To showcase the versatility, lets look at an example:
	//
	//    metrics_relabel_configs:
	//      # this will drop the `replica` label; enabling replica deduplication
	//      # similar to thanos -- https://github.com/thanos-io/thanos/blob/master/docs/components/query.md#deduplication
	//      - action: labeldrop
	//        source_label: replica
	//      # this will replace the `job` label with `scrape_job`
	//      - action: replace
	//        source_label: job
	//        target_label: scrape_job
	//      # this will drop the label `job`.
	//      - action: labeldrop
	//        source_label: job
	//      # this will lowecase the `branch` label in-place (as source_label and target_label match)
	//      - action: lowercase
	//        source_label: branch
	//        target_label: branch
	//      # this will uppercase the `instance` label into `instanceUpper`
	//      - action: uppercase
	//        source_label: instance
	//        target_label: instanceUpper
	MetricsRelabelConfigs []*promclient.MetricRelabelConfig `yaml:"metrics_relabel_configs,omitempty"`
	// ServiceDiscoveryConfigs is a set of ServiceDiscoveryConfig options that allow promxy to discover
	// all hosts in the server_group
	ServiceDiscoveryConfigs discovery.Configs `yaml:"-"`
	// PathPrefix to prepend to all queries to hosts in this servergroup
	PathPrefix string `yaml:"path_prefix"`
	// QueryParams are a map of query params to add to all HTTP calls made to this downstream
	// the main use-case for this is to add `nocache=1` to VictoriaMetrics downstreams
	// (see https://github.com/jacksontj/promxy/issues/202)
	QueryParams map[string]string `yaml:"query_params"`
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
	AntiAffinity time.Duration `yaml:"anti_affinity,omitempty"`

	// Timeout, if non-zero, specifies the amount of
	// time to wait for a server's response headers after fully
	// writing the request (including its body, if any). This
	// time does not include the time to read the response body.
	Timeout time.Duration `yaml:"timeout,omitempty"`

	// MaxIdleConns, servergroup maximum number of idle connections to keep open.
	MaxIdleConns int `yaml:"max_idle_conns,omitempty"`

	// MaxIdleConnsPerHost, servergroup maximum number of idle connections to keep open per host.
	MaxIdleConnsPerHost int `yaml:"max_idle_conns_per_host,omitempty"`

	// IdleConnTimeout, time wait to close a idle connections.
	IdleConnTimeout time.Duration `yaml:"idle_conn_timeout,omitempty"`

	// IgnoreError will hide all errors from this given servergroup effectively making
	// the responses from this servergroup "not required" for the result.
	// Note: this allows you to make the tradeoff between availability of queries and consistency of results
	IgnoreError bool `yaml:"ignore_error"`

	// DowngradeError converts all errors to warnings from this given servergroup effectively making
	// the responses from this servergroup "not required" for the result.
	// Note: this allows you to make the tradeoff between availability of queries and consistency of results
	DowngradeError bool `yaml:"downgrade_error"`

	// RelativeTimeRangeConfig defines a relative time range that this servergroup will respond to
	// An example use-case would be if a specific servergroup was long-term storage, it might only
	// have data 3d old and retain 90d of data.
	RelativeTimeRangeConfig *RelativeTimeRangeConfig `yaml:"relative_time_range"`

	// AbsoluteTimeRangeConfig defines an absolute time range that this servergroup will respond to
	// An example use-case would be if a specific servergroup was was "deprecated" and wasn't getting
	// any new data after a specific given point in time
	AbsoluteTimeRangeConfig *AbsoluteTimeRangeConfig `yaml:"absolute_time_range"`

	// LabelFilterConfig is a mechanism to restrict which queries are sent to the particular downstream.
	// This is done by maintaining a "filter" of labels that are downstream and ensuring that the
	// matchers for any particular query match that in-memory filter. This can be defined both
	// statically and dynamically.
	// NOTE: this is not a "secure" mechanism as it is relying on the query's matchers. So it is trivial
	// for a malicious actor to work around this filter by changing matchers.
	// Example:
	//
	//    label_filter:
	//      # This will dynamically query the downstream for the values of `__name__` and `job`
	//      dynamic_labels:
	//        - __name__
	//        - job
	//      # (optional) this will define a re-sync interval for dynamic labels from the downstream
	//      sync_interval: 5m
	//      # This will statically define a filter of labels
	//      static_labels_include:
	//        instance:
	//          - instance1
	//      # This will statically define an exclusion list (removed from the filter)|
	//      static_labels_exclude:
	//        __name__:
	//          - up

	LabelFilterConfig *promclient.LabelFilterConfig `yaml:"label_filter"`

	PreferMax bool `yaml:"prefer_max,omitempty"`

	// HTTPClientHeaders are a map of HTTP headers to add to remote read HTTP calls made to this downstream
	// the main use-case for this is to support the X-Scope-OrgID header required by Mimir and Cortex
	// in multi-tenancy mode
	// (see https://github.com/jacksontj/promxy/issues/643)
	HTTPClientHeaders map[string]string `yaml:"http_headers"`
}

// GetScheme returns the scheme for this servergroup
func (c *Config) GetScheme() string {
	return c.Scheme
}

// GetAntiAffinity returns the AntiAffinity time for this servergroup
func (c *Config) GetAntiAffinity() model.Time {
	return model.TimeFromUnix(int64((c.AntiAffinity).Seconds()))
}

// GetPreferMax returns the PreferMax setting for this servergroup
func (c *Config) GetPreferMax() bool {
	return c.PreferMax
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	if err := discovery.UnmarshalYAMLWithInlineConfigs(c, unmarshal); err != nil {
		return err
	}

	return nil
}

// MarshalYAML implements the yaml.Marshaler interface.
func (c *Config) MarshalYAML() (interface{}, error) {
	return discovery.MarshalYAMLWithInlineConfigs(c)
}

// HTTPClientConfig extends prometheus' HTTPClientConfig
type HTTPClientConfig struct {
	DialTimeout time.Duration                `yaml:"dial_timeout"`
	HTTPConfig  config_util.HTTPClientConfig `yaml:",inline"`
}

// RelativeTimeRangeConfig configures durations relative from "now" to define
// a servergroup's time range
type RelativeTimeRangeConfig struct {
	Start    *time.Duration `yaml:"start"`
	End      *time.Duration `yaml:"end"`
	Truncate bool           `yaml:"truncate"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (tr *RelativeTimeRangeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain RelativeTimeRangeConfig
	if err := unmarshal((*plain)(tr)); err != nil {
		return err
	}

	return tr.validate()
}

func (tr *RelativeTimeRangeConfig) validate() error {
	if tr.End != nil && tr.Start != nil && *tr.End < *tr.Start {
		return fmt.Errorf("RelativeTimeRangeConfig: End must be after start")
	}
	return nil
}

// AbsoluteTimeRangeConfig contains absolute times to define a servergroup's time range
type AbsoluteTimeRangeConfig struct {
	Start    time.Time `yaml:"start"`
	End      time.Time `yaml:"end"`
	Truncate bool      `yaml:"truncate"`
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (tr *AbsoluteTimeRangeConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain AbsoluteTimeRangeConfig
	if err := unmarshal((*plain)(tr)); err != nil {
		return err
	}

	return tr.validate()
}

func (tr *AbsoluteTimeRangeConfig) validate() error {
	if !tr.Start.IsZero() && !tr.End.IsZero() && tr.End.Before(tr.Start) {
		return fmt.Errorf("AbsoluteTimeRangeConfig: End must be after start")
	}
	return nil
}
