package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

func clearErr(error) error { return nil }

// IgnoreErrorAPI simply swallows all errors from the given API. This allows the API to
// be used with all the regular error merging logic and effectively have its errors
// not considered
type IgnoreErrorAPI struct {
	A API
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (n *IgnoreErrorAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	v, w, _ := n.A.LabelNames(ctx, matchers, startTime, endTime)
	return v, w, nil
}

// LabelValues performs a query for the values of the given label.
func (n *IgnoreErrorAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	v, w, _ := n.A.LabelValues(ctx, label, matchers, startTime, endTime)

	return v, w, nil
}

// Query performs a query for the given time.
func (n *IgnoreErrorAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return MapErrSeriesSet(n.A.Query(ctx, query, ts), clearErr)
}

// QueryRange performs a query for the given range.
func (n *IgnoreErrorAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return MapErrSeriesSet(n.A.QueryRange(ctx, query, r), clearErr)
}

// Series finds series by label matchers.
func (n *IgnoreErrorAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	v, w, _ := n.A.Series(ctx, matches, startTime, endTime)

	return v, w, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (n *IgnoreErrorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return MapErrSeriesSet(n.A.GetValue(ctx, start, end, matchers), clearErr)
}

// Key returns a labelset used to determine other api clients that are the "same"
func (n *IgnoreErrorAPI) Key() model.LabelSet {
	if apiLabels, ok := n.A.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (n *IgnoreErrorAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	v, _ := n.A.Metadata(ctx, metric, limit)
	return v, nil
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (n *IgnoreErrorAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	v, _ := n.A.QueryExemplars(ctx, query, startTime, endTime)
	return v, nil
}
