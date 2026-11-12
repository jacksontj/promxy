package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// MetricAPIFunc records the outcome of a single call made through a MetricAPI.
type MetricAPIFunc func(api, status string, took float64)

// MetricAPI reports every call made to the wrapped API through MetricFunc.
//
// It is meant to wrap the downstream client directly, beneath any wrapper that
// can answer a call without forwarding it (time range filters, label matching,
// label_filter), so that only calls actually sent downstream are reported.
type MetricAPI struct {
	A          API
	MetricFunc MetricAPIFunc
}

func (m *MetricAPI) record(api string, start time.Time, err error) {
	took := time.Since(start)
	status := "success"
	if err != nil {
		status = "error"
	}
	m.MetricFunc(api, status, took.Seconds())
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (m *MetricAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	start := time.Now()
	v, w, err := m.A.LabelNames(ctx, matchers, startTime, endTime)
	m.record("label_names", start, err)
	return v, w, err
}

// LabelValues performs a query for the values of the given label.
func (m *MetricAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	start := time.Now()
	v, w, err := m.A.LabelValues(ctx, label, matchers, startTime, endTime)
	m.record("label_values", start, err)
	return v, w, err
}

// Query performs a query for the given time.
func (m *MetricAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	start := time.Now()
	ss := m.A.Query(ctx, query, ts)
	m.record("query", start, ss.Err())
	return ss
}

// QueryRange performs a query for the given range.
func (m *MetricAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	start := time.Now()
	ss := m.A.QueryRange(ctx, query, r)
	m.record("query_range", start, ss.Err())
	return ss
}

// Series finds series by label matchers.
func (m *MetricAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	start := time.Now()
	v, w, err := m.A.Series(ctx, matches, startTime, endTime)
	m.record("series", start, err)
	return v, w, err
}

// GetValue loads the raw data for a given set of matchers in the time range
func (m *MetricAPI) GetValue(ctx context.Context, startTime, endTime time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	start := time.Now()
	ss := m.A.GetValue(ctx, startTime, endTime, matchers)
	m.record("get_value", start, ss.Err())
	return ss
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (m *MetricAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	start := time.Now()
	v, err := m.A.Metadata(ctx, metric, limit)
	m.record("metadata", start, err)
	return v, err
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (m *MetricAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	start := time.Now()
	v, err := m.A.QueryExemplars(ctx, query, startTime, endTime)
	m.record("query_exemplars", start, err)
	return v, err
}
