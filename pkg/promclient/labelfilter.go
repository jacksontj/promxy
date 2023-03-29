package promclient

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/sirupsen/logrus"
)

// Metrics
var (
	syncCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "promxy_label_filter_sync_count_total",
		Help: "How many syncs completed from a promxy label_filter, partitioned by success",
	}, []string{"status"})
	syncSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "promxy_label_filter_sync_duration_seconds",
		Help: "Latency of sync process from a promxy label_fitler",
	}, []string{"status"})
	filteredCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "promxy_label_filter_filtered_count_total",
		Help: "How many requests have been filtered from the downstream,, partitioned by query type",
	}, []string{"type"})
)

func init() {
	prometheus.MustRegister(
		syncCount,
		syncSummary,
		filteredCount,
	)
}

// LabelFilterConfig is the configuration for the LabelFilterClient
type LabelFilterConfig struct {
	// DynamicLabels is a list of labels to dynamically maintain a filter from the downstream from
	DynamicLabels []string `yaml:"dynamic_labels"`
	// SyncInterval defines how frequenlty to update the dynamic label filter
	SyncInterval time.Duration `yaml:"sync_interval"`
	// StaticLabelsInclude is a set of labels to always add to the downstream filter
	// this allows you to define some metrics to be included statically if you want to
	// avoid polling the downstream.
	// NOTE: this is not a "secure" measure as this entire label_filter is based on matchers
	// and as such doesn't restrict which metrics they touch (e.g. if you restrict by `__name__`
	// the could just query by another label).
	StaticLabelsInclude map[string][]string `yaml:"static_labels_include"`
	// StaticLabelsExclude is a set of labels to always exclude from the filter. This is done last
	// so it will apply after the dynamic and static lists are added to the filter.
	StaticLabelsExclude map[string][]string `yaml:"static_labels_exclude"`
}

func (c *LabelFilterConfig) Validate() error {
	for _, l := range c.DynamicLabels {
		if !model.IsValidMetricName(model.LabelValue(l)) {
			return fmt.Errorf("%s is not a valid label name", l)
		}
	}

	if c.SyncInterval > 0 && len(c.DynamicLabels) == 0 {
		return fmt.Errorf("sync_interval requires `dynamic_labels_include` to be set")
	}

	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (c *LabelFilterConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type plain LabelFilterConfig
	if err := unmarshal((*plain)(c)); err != nil {
		return err
	}

	return c.Validate()
}

// NewLabelFilterClient returns a LabelFilterClient which will filter the queries sent downstream based
// on a filter of labels maintained in memory from the downstream API.
func NewLabelFilterClient(ctx context.Context, a API, cfg *LabelFilterConfig) (*LabelFilterClient, error) {
	c := &LabelFilterClient{
		API: a,
		ctx: ctx,
		cfg: cfg,
	}

	// Do an initial sync
	if err := c.Sync(ctx); err != nil {
		return nil, err
	}

	if cfg.SyncInterval > 0 {
		go func() {
			ticker := time.NewTicker(cfg.SyncInterval)
			for {
				select {
				case <-ticker.C:
					start := time.Now()
					err := c.Sync(ctx)
					took := time.Since(start)
					status := "success"
					if err != nil {
						logrus.Errorf("error syncing in label_filter from downstream: %#v", err)
						status = "error"
					}
					syncCount.WithLabelValues(status).Inc()
					syncSummary.WithLabelValues(status).Observe(took.Seconds())

				case <-ctx.Done():
					ticker.Stop()
					return
				}
			}
		}()
	}

	return c, nil
}

// LabelFilterClient filters out calls to the downstream based on a label filter
// which is pulled and maintained from the downstream API.
type LabelFilterClient struct {
	API

	// filter is an atomic to hold the LabelFilter which is a map of labelName -> labelValue -> nothing (for quick lookups)
	filter atomic.Value

	// Used as the background context for this client
	ctx context.Context

	// cfg is a pointer to the config for this client
	cfg *LabelFilterConfig
}

// State returns the current ServerGroupState
func (c *LabelFilterClient) LabelFilter() map[string]map[string]struct{} {
	tmp := c.filter.Load()
	if ret, ok := tmp.(map[string]map[string]struct{}); ok {
		return ret
	}
	return nil
}

func (c *LabelFilterClient) Sync(ctx context.Context) error {
	filter := make(map[string]map[string]struct{})

	for _, label := range c.cfg.DynamicLabels {
		labelFilter := make(map[string]struct{})
		// TODO: warn?
		vals, _, err := c.LabelValues(ctx, label, nil, model.Time(0).Time(), model.Now().Time())
		if err != nil {
			return err
		}
		for _, v := range vals {
			labelFilter[string(v)] = struct{}{}
		}
		filter[label] = labelFilter
	}

	// Apply static include list
	for k, vList := range c.cfg.StaticLabelsInclude {
		filterMap, ok := filter[k]
		if !ok {
			filterMap = make(map[string]struct{})
		}
		for _, item := range vList {
			filterMap[item] = struct{}{}
		}
		filter[k] = filterMap
	}

	// Apply exclude list
	for k, vList := range c.cfg.StaticLabelsExclude {
		if filterMap, ok := filter[k]; ok {
			for _, item := range vList {
				delete(filterMap, item)
			}
			filter[k] = filterMap
		}
	}

	c.filter.Store(filter)

	return nil
}

// Query performs a query for the given time.
func (c *LabelFilterClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return nil, nil, err
	}

	filterVisitor := NewFilterLabelVisitor(c.LabelFilter())
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, nil, err
	}
	if !filterVisitor.filterMatch {
		filteredCount.WithLabelValues("Query").Inc()
		return nil, nil, nil
	}

	return c.API.Query(ctx, query, ts)
}

// Query performs a query for the given time.
func (c *LabelFilterClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return nil, nil, err
	}

	filterVisitor := NewFilterLabelVisitor(c.LabelFilter())
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, nil, err
	}
	if !filterVisitor.filterMatch {
		filteredCount.WithLabelValues("QueryRange").Inc()
		return nil, nil, nil
	}

	return c.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (c *LabelFilterClient) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	for _, m := range matches {
		matchers, err := parser.ParseMetricSelector(m)
		if err != nil {
			return nil, nil, err
		}
		// check if the matcher is excluded by our filter
		for _, matcher := range matchers {
			if !FilterLabelMatchers(c.LabelFilter(), matcher) {
				filteredCount.WithLabelValues("Series").Inc()
				return nil, nil, nil
			}
		}
	}
	return c.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *LabelFilterClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	// check if the matcher is excluded by our filter
	for _, matcher := range matchers {
		if !FilterLabelMatchers(c.LabelFilter(), matcher) {
			filteredCount.WithLabelValues("GetValue").Inc()
			return nil, nil, nil
		}
	}
	return c.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (c *LabelFilterClient) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	matcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, metric)
	if err != nil {
		return nil, err
	}
	if !FilterLabelMatchers(c.LabelFilter(), matcher) {
		filteredCount.WithLabelValues("Metadata").Inc()
		return nil, nil
	}
	return c.API.Metadata(ctx, metric, limit)
}

func NewFilterLabelVisitor(filter map[string]map[string]struct{}) *FilterLabelVisitor {
	return &FilterLabelVisitor{
		labelFilter: filter,
		filterMatch: true,
	}
}

// FilterLabel implements the parser.Visitor interface to filter selectors based on a labelstet
type FilterLabelVisitor struct {
	l           sync.Mutex
	labelFilter map[string]map[string]struct{}
	filterMatch bool
}

// Visit checks if the given node matches the labels in the filter
func (l *FilterLabelVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
		for _, matcher := range nodeTyped.LabelMatchers {
			if !FilterLabelMatchers(l.labelFilter, matcher) {
				l.l.Lock()
				l.filterMatch = false
				l.l.Unlock()
				return nil, nil
			}
		}
	}

	return l, nil
}

// TODO: better name, this is to check if a matcher is in the filter
func FilterLabelMatchers(filter map[string]map[string]struct{}, matcher *labels.Matcher) bool {
	for labelName, labelFilter := range filter {
		if matcher.Name == labelName {
			match := false
			// Check that there is a match somewhere!
			for v := range labelFilter {
				if matcher.Matches(v) {
					match = true
					break
				}
			}
			if !match {
				return match
			}
		}
	}

	return true
}
