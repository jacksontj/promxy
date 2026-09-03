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
	"github.com/prometheus/prometheus/storage"
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

// LabelFilterOnSyncError defines what a LabelFilterClient does while it has
// never successfully synced its filter from the downstream (e.g. the target is
// unreachable when promxy starts up).
type LabelFilterOnSyncError string

const (
	// LabelFilterOnSyncErrorAbort fails the initial sync, which propagates up to
	// the servergroup and blocks startup until a sync succeeds. This preserves
	// the historical behavior and is the default.
	LabelFilterOnSyncErrorAbort LabelFilterOnSyncError = "abort"
	// LabelFilterOnSyncErrorOpen lets startup proceed and sends all queries
	// downstream (i.e. no filtering) until the first successful sync.
	LabelFilterOnSyncErrorOpen LabelFilterOnSyncError = "open"
	// LabelFilterOnSyncErrorClosed lets startup proceed but filters out every
	// query (i.e. the target is skipped) until the first successful sync.
	LabelFilterOnSyncErrorClosed LabelFilterOnSyncError = "closed"
)

// defaultSyncRetryInterval is how often the filter retries to obtain its first
// successful sync when no explicit sync_interval is configured.
const defaultSyncRetryInterval = 5 * time.Second

// LabelFilterConfig is the configuration for the LabelFilterClient
type LabelFilterConfig struct {
	// DynamicLabels is a list of labels to dynamically maintain a filter from the downstream from
	DynamicLabels []string `yaml:"dynamic_labels"`
	// SyncInterval defines how frequenlty to update the dynamic label filter
	SyncInterval time.Duration `yaml:"sync_interval"`
	// SyncLookback bounds how far back each dynamic label sync looks on the
	// downstream; the sync asks for label values over `now-sync_lookback` to
	// `now` instead of over all time.
	//
	// The default (unset/0) is unbounded: every sync asks the downstream for the
	// values of each dynamic label from the epoch to now. That is correct
	// regardless of the downstream's retention, but it is also the most
	// expensive question you can ask -- the downstream has to consult every
	// block on disk, for every dynamic label, for every target in the
	// servergroup, once per `sync_interval`. For the common `__name__` case that
	// is the complete set of metric names the downstream has ever held. Setting
	// this to something comfortably larger than your scrape/staleness window
	// (e.g. 1h or 24h) bounds that work to the recent blocks.
	//
	// NOTE: a label value that exists downstream but has had no samples within
	// the lookback window will be absent from the filter, and queries matching
	// only that value will be filtered out (not sent downstream) -- so keep this
	// well above the age of data you expect to query from this target.
	SyncLookback time.Duration `yaml:"sync_lookback"`
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
	// OnSyncError controls behavior while the filter has never successfully synced
	// from the downstream (e.g. the target is unreachable at startup). This is
	// distinct from the servergroup's `ignore_error`, which governs the query path.
	//   abort  - fail the sync; this blocks servergroup startup until a sync
	//            succeeds (default; preserves historical behavior)
	//   open   - proceed without filtering; all queries are sent downstream until
	//            the first successful sync
	//   closed - proceed but filter out everything (skip this target) until the
	//            first successful sync
	OnSyncError LabelFilterOnSyncError `yaml:"on_sync_error"`
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

	if c.SyncLookback < 0 {
		return fmt.Errorf("sync_lookback must not be negative")
	}

	if c.SyncLookback > 0 && len(c.DynamicLabels) == 0 {
		return fmt.Errorf("sync_lookback requires `dynamic_labels` to be set")
	}

	switch c.OnSyncError {
	case "":
		c.OnSyncError = LabelFilterOnSyncErrorAbort
	case LabelFilterOnSyncErrorAbort, LabelFilterOnSyncErrorOpen, LabelFilterOnSyncErrorClosed:
	default:
		return fmt.Errorf("invalid on_sync_error %q, must be one of: abort, open, closed", c.OnSyncError)
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

	// Do an initial sync. If it fails, behavior depends on on_sync_error:
	//   abort       -> return the error, which blocks servergroup startup until a
	//                  sync succeeds (historical behavior, and the default).
	//   open/closed -> log and continue with an unloaded filter; queries are
	//                  passed through (open) or filtered out (closed) until a
	//                  background sync succeeds.
	if err := c.Sync(ctx); err != nil {
		if cfg.OnSyncError != LabelFilterOnSyncErrorOpen && cfg.OnSyncError != LabelFilterOnSyncErrorClosed {
			return nil, err
		}
		logrus.Errorf("error in initial label_filter sync from downstream (on_sync_error=%s), continuing and retrying in the background: %v", cfg.OnSyncError, err)
	}

	// Run a background sync loop when either a periodic sync_interval is
	// configured, or the initial sync failed and we need to keep retrying until
	// the first successful sync.
	if cfg.SyncInterval > 0 || c.LabelFilter() == nil {
		go c.syncLoop(ctx)
	}

	return c, nil
}

// syncLoop periodically re-syncs the filter from the downstream. When a
// sync_interval is configured it runs forever at that cadence; when it is not,
// the loop only exists to obtain the first successful sync (e.g. after a failed
// initial sync) and exits once a filter has been loaded.
func (c *LabelFilterClient) syncLoop(ctx context.Context) {
	for {
		interval := c.cfg.SyncInterval
		if interval <= 0 {
			interval = defaultSyncRetryInterval
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(interval):
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

			// With no configured sync_interval we're only retrying to obtain the
			// first successful sync; stop once we have a filter loaded.
			if c.cfg.SyncInterval <= 0 && c.LabelFilter() != nil {
				return
			}
		}
	}
}

// blocked reports whether the filter has never successfully synced and is
// configured to fail closed, in which case all downstream calls are skipped
// (the target is treated as "down" until the first successful sync).
func (c *LabelFilterClient) blocked() bool {
	return c.cfg != nil && c.cfg.OnSyncError == LabelFilterOnSyncErrorClosed && c.LabelFilter() == nil
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

	end := model.Now().Time()
	// An unset sync_lookback means "all of time"; the epoch is as far back as a
	// model.Time can express, so it stands in for an unbounded start.
	start := model.Time(0).Time()
	if c.cfg.SyncLookback > 0 {
		start = end.Add(-c.cfg.SyncLookback)
	}

	for _, label := range c.cfg.DynamicLabels {
		labelFilter := make(map[string]struct{})
		// TODO: warn?
		vals, _, err := c.LabelValues(ctx, label, nil, start, end)
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
func (c *LabelFilterClient) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if c.blocked() {
		filteredCount.WithLabelValues("Query").Inc()
		return storage.EmptySeriesSet()
	}

	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	filterVisitor := NewFilterLabelVisitor(c.LabelFilter())
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return storage.ErrSeriesSet(err)
	}
	if !filterVisitor.filterMatch {
		filteredCount.WithLabelValues("Query").Inc()
		return storage.EmptySeriesSet()
	}

	return c.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (c *LabelFilterClient) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	if c.blocked() {
		filteredCount.WithLabelValues("QueryRange").Inc()
		return storage.EmptySeriesSet()
	}

	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	filterVisitor := NewFilterLabelVisitor(c.LabelFilter())
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return storage.ErrSeriesSet(err)
	}
	if !filterVisitor.filterMatch {
		filteredCount.WithLabelValues("QueryRange").Inc()
		return storage.EmptySeriesSet()
	}

	return c.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (c *LabelFilterClient) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if c.blocked() {
		filteredCount.WithLabelValues("Series").Inc()
		return nil, nil, nil
	}
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
func (c *LabelFilterClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	if c.blocked() {
		filteredCount.WithLabelValues("GetValue").Inc()
		return storage.EmptySeriesSet()
	}
	// check if the matcher is excluded by our filter
	for _, matcher := range matchers {
		if !FilterLabelMatchers(c.LabelFilter(), matcher) {
			filteredCount.WithLabelValues("GetValue").Inc()
			return storage.EmptySeriesSet()
		}
	}
	return c.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (c *LabelFilterClient) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	if c.blocked() {
		filteredCount.WithLabelValues("Metadata").Inc()
		return nil, nil
	}
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

// QueryExemplars performs a query for exemplars by the given query and time range.
// Mirrors the matcher-filter logic used by Series / GetValue: parse the
// query, extract every vector selector, and skip the downstream call when
// every selector references labels this server-group can't satisfy. A
// query that mixes a satisfiable selector with a non-satisfiable one is
// still forwarded (we can't easily rewrite a PromQL string) — the
// non-satisfiable side just returns no exemplars.
func (c *LabelFilterClient) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	if c.blocked() {
		filteredCount.WithLabelValues("QueryExemplars").Inc()
		return nil, nil
	}
	expr, err := parser.ParseExpr(query)
	if err != nil {
		// Parse error: let the downstream return the canonical error rather
		// than swallowing it here.
		return c.API.QueryExemplars(ctx, query, startTime, endTime)
	}
	selectors := parser.ExtractSelectors(expr)
	if len(selectors) == 0 {
		return c.API.QueryExemplars(ctx, query, startTime, endTime)
	}
	for _, ms := range selectors {
		ok := true
		for _, matcher := range ms {
			if !FilterLabelMatchers(c.LabelFilter(), matcher) {
				ok = false
				break
			}
		}
		if ok {
			return c.API.QueryExemplars(ctx, query, startTime, endTime)
		}
	}
	filteredCount.WithLabelValues("QueryExemplars").Inc()
	return nil, nil
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

// FilterLabelMatchers returns whether the given matcher can be satisfied by the
// filter; meaning whether at least one value in the filter's value-set for
// `matcher.Name` matches. If the filter has no entry at all for the matcher's
// label name then we know nothing about that label, so we return true (meaning
// "don't filter this out").
//
// This is semantically identical to scanning the value set calling
// `matcher.Matches(v)` on every value, but avoids doing so wherever the answer
// can be computed from map lookups instead -- the value sets here can be very
// large (e.g. every `__name__` on the downstream) and this is called per
// matcher, per target, per query, before any I/O is done.
//
// TODO: better name, this is to check if a matcher is in the filter
func FilterLabelMatchers(filter map[string]map[string]struct{}, matcher *labels.Matcher) bool {
	labelFilter, ok := filter[matcher.Name]
	// If we have no filter for this label; we have nothing to say about it
	if !ok {
		return true
	}

	switch matcher.Type {
	// An equality matcher matches exactly one value; so this is a single lookup
	case labels.MatchEqual:
		_, ok := labelFilter[matcher.Value]
		return ok

	// A not-equal matcher matches every value *except* one; so it is satisfied
	// as long as the set holds something other than that one value
	case labels.MatchNotEqual:
		switch len(labelFilter) {
		case 0:
			return false
		case 1:
			// The lone value in the set is either the excluded one (no match) or
			// something else (match)
			_, ok := labelFilter[matcher.Value]
			return !ok
		default:
			// More than one distinct value; at least one of them isn't the
			// excluded one
			return true
		}

	// For regexes upstream can often tell us the exact set of literal values the
	// pattern matches (e.g. `a|b|c`); which turns this back into map lookups
	case labels.MatchRegexp:
		if setMatches := matcher.SetMatches(); len(setMatches) > 0 {
			for _, v := range setMatches {
				if _, ok := labelFilter[v]; ok {
					return true
				}
			}
			return false
		}

	case labels.MatchNotRegexp:
		if setMatches := matcher.SetMatches(); len(setMatches) > 0 {
			// The matcher matches everything *except* setMatches; so it is
			// satisfied iff the filter holds some value outside of that set
			return countPresent(labelFilter, setMatches) < len(labelFilter)
		}
	}

	// A genuine regex; we have no choice but to check the values in the set
	for v := range labelFilter {
		if matcher.Matches(v) {
			return true
		}
	}
	return false
}

// countPresent returns how many *distinct* values from `values` are present in
// `set`. `set` is never mutated (it is shared across goroutines).
func countPresent(set map[string]struct{}, values []string) int {
	count := 0
	// Only allocated if we actually find duplicates worth tracking
	var seen map[string]struct{}
	for i, v := range values {
		if _, ok := set[v]; !ok {
			continue
		}
		if seen == nil {
			// The first hit can't be a duplicate; subsequent ones might be
			seen = make(map[string]struct{}, len(values)-i)
		} else if _, dup := seen[v]; dup {
			continue
		}
		seen[v] = struct{}{}
		count++
	}
	return count
}
