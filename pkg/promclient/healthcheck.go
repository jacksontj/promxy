package promclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

// Metrics
var (
	healthCheckUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "promxy_health_check_up",
		Help: "Whether the most recent health_check probe(s) consider this target healthy (1) or not (0)",
	}, []string{"target"})
	healthCheckProbeCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "promxy_health_check_probe_total",
		Help: "How many health_check probes completed against a target, partitioned by outcome",
	}, []string{"target", "status"})
	healthCheckSkippedCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "promxy_health_check_skipped_total",
		Help: "How many requests have been skipped because a target was unhealthy, partitioned by query type",
	}, []string{"target", "type"})
)

func init() {
	prometheus.MustRegister(
		healthCheckUp,
		healthCheckProbeCount,
		healthCheckSkippedCount,
	)
}

// ErrTargetUnhealthy is returned by every wrapped method while a target's
// health_check probe(s) are failing, so MultiAPI and ignore_error/
// downgrade_error treat this target as down instead of a successful empty response.
var ErrTargetUnhealthy = errors.New("promclient: target failed health_check probe(s)")

const (
	// defaultHealthCheckPath is probed when Path is unset. It is prometheus'
	// own liveness endpoint, present on any prometheus-compatible downstream.
	defaultHealthCheckPath = "/-/healthy"
	// defaultHealthCheckInterval is used when Interval is unset.
	defaultHealthCheckInterval = 10 * time.Second
	// defaultHealthCheckTimeout is used when Timeout is unset -- independent of
	// http_client.timeout so an unresponsive target is caught quickly regardless.
	defaultHealthCheckTimeout = 2 * time.Second
	// defaultHealthCheckThreshold is used when FailureThreshold/SuccessThreshold
	// are unset.
	defaultHealthCheckThreshold = 1
)

// HealthCheckConfig configures a periodic HTTP probe against a target. While
// unhealthy, calls return ErrTargetUnhealthy instead of hitting the downstream
// -- excluding the target from MultiAPI/ignore_error/downgrade_error like a
// genuine failure, and avoiding hangs against a target that accepts TCP but
// never answers HTTP.
//
// Example:
//
//	health_check:
//	  path: /-/healthy
//	  interval: 10s
//	  timeout: 2s
//	  failure_threshold: 1
//	  success_threshold: 1
type HealthCheckConfig struct {
	// Path is the URL path probed on the target, joined onto the target's base
	// URL (scheme+host+path_prefix). Defaults to "/-/healthy".
	Path string `yaml:"path,omitempty"`
	// Interval is how often the target is probed. Defaults to 10s.
	Interval time.Duration `yaml:"interval,omitempty"`
	// Timeout bounds each probe request, independent of http_client.timeout.
	// Defaults to 2s.
	Timeout time.Duration `yaml:"timeout,omitempty"`
	// FailureThreshold is the number of consecutive failed probes required
	// before the target is marked unhealthy. Defaults to 1.
	FailureThreshold int `yaml:"failure_threshold,omitempty"`
	// SuccessThreshold is the number of consecutive successful probes required
	// before a previously-unhealthy target is marked healthy again. Defaults to 1.
	SuccessThreshold int `yaml:"success_threshold,omitempty"`
}

// Validate fills in defaults and checks for invalid configuration.
func (c *HealthCheckConfig) Validate() error {
	if c.Path == "" {
		c.Path = defaultHealthCheckPath
	}
	if c.Interval <= 0 {
		c.Interval = defaultHealthCheckInterval
	}
	if c.Timeout <= 0 {
		c.Timeout = defaultHealthCheckTimeout
	}
	if c.FailureThreshold <= 0 {
		c.FailureThreshold = defaultHealthCheckThreshold
	}
	if c.SuccessThreshold <= 0 {
		c.SuccessThreshold = defaultHealthCheckThreshold
	}
	if c.Timeout >= c.Interval {
		return fmt.Errorf("health_check timeout (%s) must be less than interval (%s)", c.Timeout, c.Interval)
	}
	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *HealthCheckConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain HealthCheckConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}
	return c.Validate()
}

// NewHealthCheckClient returns a HealthCheckClient that periodically probes
// targetURL+cfg.Path using httpClient and skips calls to the wrapped API
// while the target is considered unhealthy. The probe loop runs until ctx is
// done.
func NewHealthCheckClient(ctx context.Context, a API, cfg *HealthCheckConfig, targetURL string, httpClient *http.Client) *HealthCheckClient {
	c := &HealthCheckClient{
		API:        a,
		cfg:        cfg,
		target:     targetURL,
		probeURL:   targetURL + cfg.Path,
		httpClient: httpClient,
	}
	// Assume healthy until the first probe completes, so a target isn't
	// skipped during the brief startup window before any probe has run.
	c.healthy.Store(true)
	healthCheckUp.WithLabelValues(c.target).Set(1)

	go c.probeLoop(ctx)

	return c
}

// HealthCheckClient skips calls to the downstream while a periodic,
// independently-timed HTTP probe considers the target unhealthy.
type HealthCheckClient struct {
	API

	cfg        *HealthCheckConfig
	target     string
	probeURL   string
	httpClient *http.Client

	healthy atomic.Bool

	// consecutive tracks the current streak of same-outcome probes (positive
	// for successes, negative for failures), used to apply
	// FailureThreshold/SuccessThreshold before flipping healthy.
	consecutive atomic.Int32
}

// Healthy reports whether the most recent probe(s) consider this target healthy.
func (c *HealthCheckClient) Healthy() bool {
	return c.healthy.Load()
}

func (c *HealthCheckClient) probeLoop(ctx context.Context) {
	ticker := time.NewTicker(c.cfg.Interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.probe(ctx)
		}
	}
}

func (c *HealthCheckClient) probe(ctx context.Context) {
	probeCtx, cancel := context.WithTimeout(ctx, c.cfg.Timeout)
	defer cancel()

	ok := c.doProbe(probeCtx)

	status := "failure"
	if ok {
		status = "success"
	}
	healthCheckProbeCount.WithLabelValues(c.target, status).Inc()

	if ok {
		if c.consecutive.Load() < 0 {
			c.consecutive.Store(0)
		}
		streak := c.consecutive.Add(1)
		if !c.healthy.Load() && streak >= int32(c.cfg.SuccessThreshold) {
			c.healthy.Store(true)
			healthCheckUp.WithLabelValues(c.target).Set(1)
			logrus.Infof("health_check: target=%s recovered, marking healthy", c.target)
			c.triggerLabelFilterSync(ctx)
		}
	} else {
		if c.consecutive.Load() > 0 {
			c.consecutive.Store(0)
		}
		streak := -c.consecutive.Add(-1)
		if c.healthy.Load() && streak >= int32(c.cfg.FailureThreshold) {
			c.healthy.Store(false)
			healthCheckUp.WithLabelValues(c.target).Set(0)
			logrus.Warnf("health_check: target=%s failed %d consecutive probes, marking unhealthy and skipping queries", c.target, streak)
		}
	}
}

// triggerLabelFilterSync kicks an immediate label_filter re-sync on recovery
// instead of waiting for sync_interval. No-op unless this client directly
// wraps a LabelFilterClient. Runs in the background so it can't delay the
// next probe tick.
func (c *HealthCheckClient) triggerLabelFilterSync(ctx context.Context) {
	lf, ok := c.API.(*LabelFilterClient)
	if !ok {
		return
	}
	go func() {
		if err := lf.Sync(ctx); err != nil {
			logrus.Errorf("health_check: target=%s error re-syncing label_filter after recovery: %v", c.target, err)
		}
	}()
}

// doProbe issues a single probe request and reports whether it succeeded
// (a 2xx status code within the given context's deadline).
func (c *HealthCheckClient) doProbe(ctx context.Context) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.probeURL, nil)
	if err != nil {
		logrus.Errorf("health_check: error building probe request for target=%s: %v", c.target, err)
		return false
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		logrus.Debugf("health_check: probe error for target=%s: %v", c.target, err)
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// Key returns a labelset used to determine other api clients that are the "same"
func (c *HealthCheckClient) Key() model.LabelSet {
	if apiLabels, ok := c.API.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
// Guarded like every other method -- the probe loop uses its own httpClient
// directly, so there's no bootstrapping concern doing so.
func (c *HealthCheckClient) LabelNames(ctx context.Context, matchers []string, startTime, endTime time.Time) ([]string, v1.Warnings, error) {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "LabelNames").Inc()
		return nil, nil, ErrTargetUnhealthy
	}
	return c.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (c *HealthCheckClient) LabelValues(ctx context.Context, label string, matchers []string, startTime, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "LabelValues").Inc()
		return nil, nil, ErrTargetUnhealthy
	}
	return c.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (c *HealthCheckClient) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "Query").Inc()
		return storage.ErrSeriesSet(ErrTargetUnhealthy)
	}
	return c.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (c *HealthCheckClient) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "QueryRange").Inc()
		return storage.ErrSeriesSet(ErrTargetUnhealthy)
	}
	return c.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (c *HealthCheckClient) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "Series").Inc()
		return nil, nil, ErrTargetUnhealthy
	}
	return c.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *HealthCheckClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "GetValue").Inc()
		return storage.ErrSeriesSet(ErrTargetUnhealthy)
	}
	return c.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (c *HealthCheckClient) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "Metadata").Inc()
		return nil, ErrTargetUnhealthy
	}
	return c.API.Metadata(ctx, metric, limit)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (c *HealthCheckClient) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	if !c.Healthy() {
		healthCheckSkippedCount.WithLabelValues(c.target, "QueryExemplars").Inc()
		return nil, ErrTargetUnhealthy
	}
	return c.API.QueryExemplars(ctx, query, startTime, endTime)
}
