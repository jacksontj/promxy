package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// DowngradeErrorAPI simply downgrades all errors into warnings from the given API.
type DowngradeErrorAPI struct {
	A API
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (n *DowngradeErrorAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	v, w, err := n.A.LabelNames(ctx, matchers, startTime, endTime)
	if err != nil {
		w = append(w, err.Error())
	}
	return v, w, nil
}

// LabelValues performs a query for the values of the given label.
func (n *DowngradeErrorAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	v, w, err := n.A.LabelValues(ctx, label, matchers, startTime, endTime)
	if err != nil {
		w = append(w, err.Error())
	}

	return v, w, nil
}

// Query performs a query for the given time.
func (n *DowngradeErrorAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	v, w, err := n.A.Query(ctx, query, ts)
	if err != nil {
		w = append(w, err.Error())
	}

	return v, w, nil
}

// QueryRange performs a query for the given range.
func (n *DowngradeErrorAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	v, w, err := n.A.QueryRange(ctx, query, r)
	if err != nil {
		w = append(w, err.Error())
	}

	return v, w, nil
}

// Series finds series by label matchers.
func (n *DowngradeErrorAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	v, w, err := n.A.Series(ctx, matches, startTime, endTime)
	if err != nil {
		w = append(w, err.Error())
	}

	return v, w, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (n *DowngradeErrorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	v, w, err := n.A.GetValue(ctx, start, end, matchers)
	if err != nil {
		w = append(w, err.Error())
	}

	return v, w, nil
}

// Key returns a labelset used to determine other api clients that are the "same"
func (n *DowngradeErrorAPI) Key() model.LabelSet {
	if apiLabels, ok := n.A.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (n *DowngradeErrorAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	v, _ := n.A.Metadata(ctx, metric, limit)
	return v, nil
}
