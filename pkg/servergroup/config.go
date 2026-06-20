package servergroup

import (
	"fmt"
	"strings"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/sigv4"

	"github.com/jacksontj/promxy/pkg/promclient"

	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/promql/parser"
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
	// Name is an optional human-readable identifier for this server group.
	Name string `yaml:"name,omitempty"`
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

	// NativeHistogram tunes how promxy serves native-histogram queries
	// against this server group. The zero value (no `native_histogram`
	// block in YAML) is "AST-only detection, fail loud if remote_read
	// can't preserve fidelity" — the safe default. See NativeHistogramConfig.
	NativeHistogram NativeHistogramConfig `yaml:"native_histogram,omitempty"`
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

	// AntiAffinityDynamic enables per-series anti-affinity inference. When
	// true, the merge layer infers each series' buffer from the median
	// inter-sample gap of the longer side and uses AntiAffinity only as the
	// fallback when there are too few samples to estimate. This is the
	// right setting when a single server_group hosts series with mixed
	// scrape intervals (e.g. a 1m-scrape job alongside a 15s-scrape one) —
	// a single static AntiAffinity is too tight for the slow series (gaps
	// look like missing data and get gap-filled, doubling count_over_time)
	// or too wide for the fast one (legitimate fresh samples get deduped).
	// See https://github.com/jacksontj/promxy/issues/734.
	AntiAffinityDynamic bool `yaml:"anti_affinity_dynamic,omitempty"`

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

	// InjectMatchers is a list of label matchers that are injected into every selector
	// of every request sent to this servergroup. This effectively scopes the servergroup
	// to the subset of downstream data matching these matchers -- even for queries that
	// never reference the labels being injected (e.g. with `cluster="A"` configured,
	// `count(up)` is sent downstream as `count(up{cluster="A"})`).
	//
	// Each entry is a single matcher in promql syntax (no enclosing braces), e.g.:
	//
	//    inject_matchers:
	//      - 'cluster="A"'
	//      - 'region=~"us-.*"'
	//
	// Unlike `labels` (which only adds labels to responses) and `label_filter` (which only
	// drops queries that can't match), `inject_matchers` always adds the matchers to the
	// queries themselves. A common use-case is presenting a per-tenant view of a merged
	// downstream (e.g. a single Mimir/Thanos/Prometheus holding many clusters) -- see
	// https://github.com/jacksontj/promxy/issues/698
	// NOTE: this is not a "secure" mechanism; it only mutates the request matchers and
	// relies on the downstream honoring them.
	InjectMatchers []string `yaml:"inject_matchers,omitempty"`

	PreferMax bool `yaml:"prefer_max,omitempty"`

	// HTTPClientHeaders are a map of HTTP headers to add to remote read HTTP calls made to this downstream
	// the main use-case for this is to support the X-Scope-OrgID header required by Mimir and Cortex
	// in multi-tenancy mode
	// (see https://github.com/jacksontj/promxy/issues/643)
	HTTPClientHeaders map[string]string `yaml:"http_headers"`

	// AlignQueryRangeWithStep declares that this backend snaps query_range results
	// to the epoch step grid (k*step), as Mimir/Cortex do by default. When set,
	// promxy re-stamps the returned samples back onto the grid implied by the
	// request start (start + j*step) so they line up with promxy's local
	// evaluation grid. Without it, an unaligned request (start % step != 0) whose
	// off-grid distance exceeds the lookback-delta yields no data for this backend.
	// Leave it OFF for backends that do not step-align (e.g. vanilla Prometheus);
	// their samples already sit on the requested grid. See issue #787.
	AlignQueryRangeWithStep bool `yaml:"align_query_range_with_step,omitempty"`
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

// GetInjectMatchers parses the configured InjectMatchers entries into label matchers.
// It returns nil if no matchers are configured.
func (c *Config) GetInjectMatchers() ([]*labels.Matcher, error) {
	if len(c.InjectMatchers) == 0 {
		return nil, nil
	}
	// Join the individual matcher entries into a single selector and parse it. Each entry
	// is expected to be a bare matcher (e.g. `cluster="A"`) without enclosing braces.
	matchers, err := parser.ParseMetricSelector("{" + strings.Join(c.InjectMatchers, ",") + "}")
	if err != nil {
		return nil, fmt.Errorf("error parsing inject_matchers: %w", err)
	}
	return matchers, nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *Config) UnmarshalYAML(unmarshal func(interface{}) error) error {
	*c = DefaultConfig

	if err := discovery.UnmarshalYAMLWithInlineConfigs(c, unmarshal); err != nil {
		return err
	}

	if err := c.validateAuthConfig(); err != nil {
		return err
	}

	// Validate inject_matchers parses at config-load time rather than failing later
	// during a discovery sync.
	if _, err := c.GetInjectMatchers(); err != nil {
		return err
	}

	return nil
}

// validateAuthConfig ensures that at most one authentication method is configured
func (c *Config) validateAuthConfig() error {
	authMethods := []string{}

	if c.HTTPConfig.HTTPConfig.BasicAuth != nil {
		authMethods = append(authMethods, "basic_auth")
	}

	if c.HTTPConfig.HTTPConfig.Authorization != nil {
		authMethods = append(authMethods, "authorization")
	}

	if len(c.HTTPConfig.HTTPConfig.BearerToken) > 0 {
		authMethods = append(authMethods, "bearer_token")
	}

	if len(c.HTTPConfig.HTTPConfig.BearerTokenFile) > 0 {
		authMethods = append(authMethods, "bearer_token_file")
	}

	if c.HTTPConfig.SigV4Config != nil {
		authMethods = append(authMethods, "sigv4")
	}

	if len(authMethods) > 1 {
		return fmt.Errorf("at most one of basic_auth, authorization, bearer_token, bearer_token_file, sigv4 must be configured")
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
	SigV4Config *sigv4.SigV4Config           `yaml:"sigv4,omitempty"`
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

// NativeHistogramConfig groups the per-server-group knobs that control how
// promxy handles native-histogram queries. Native histograms only round-trip
// with full fidelity over remote_read — the HTTP-API JSON path collapses
// sparse spans and drops empty buckets. Promxy detects histogram-bearing
// queries from the PromQL AST (and optionally from a metric-name metadata
// cache) and skips HTTP-API pushdown for them so the embedded engine
// evaluates locally, fetching raw data via remote_read.
//
// The zero value is a safe default: AST-only detection, and any histogram
// query that targets a server group without remote_read errors out rather
// than silently returning lossy data.
type NativeHistogramConfig struct {
	// MetadataRefresh controls the metric-name → type cache. When zero
	// (the default), detection is pure AST: a Call to one of the
	// histogram-only PromQL functions (histogram_avg, histogram_count,
	// histogram_sum, histogram_stddev, histogram_stdvar,
	// histogram_fraction). This misses queries that touch histogram
	// metrics without invoking those functions — e.g. `rate(my_hist[5m])`.
	//
	// Set MetadataRefresh to a positive duration to enable the cache:
	// promxy periodically calls /api/v1/metadata on this server group,
	// extracts the histogram-typed metric names, and consults the union
	// of all groups' caches when classifying queries. The cache is
	// name-keyed (typically <2% of the total metric-name space) so
	// memory is bounded by histogram-name count, not series cardinality.
	MetadataRefresh time.Duration `yaml:"metadata_refresh,omitempty"`

	// AllowLossy controls what happens when a histogram-bearing query
	// targets this server group and remote_read isn't configured.
	//
	//   false (default): the query errors out before fan-out. Wrong data
	//     is worse than no data — the operator should configure
	//     remote_read (or explicitly opt into lossy fallback) rather
	//     than silently serve histograms over the lossy JSON path.
	//
	//   true: the query proceeds via the HTTP API even though the
	//     response will be lossy for histogram samples. Use when you
	//     accept the fidelity loss (e.g. dashboards that only consume
	//     histogram_quantile output and don't care about sparse spans).
	AllowLossy bool `yaml:"allow_lossy,omitempty"`
}
