package promclient

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// IgnoreErrorAPI simply swallows all errors from the given API. This allows the API to
// be used with all the regular error merging logic and effectively have its errors
// not considered
type IgnoreErrorAPI struct {
	API
}

// LabelValues performs a query for the values of the given label.
func (n *IgnoreErrorAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	v, w, _ := n.API.LabelValues(ctx, label)

	return v, w, nil
}

// Query performs a query for the given time.
func (n *IgnoreErrorAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	v, w, _ := n.API.Query(ctx, query, ts)

	return v, w, nil
}

// QueryRange performs a query for the given range.
func (n *IgnoreErrorAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	v, w, _ := n.API.QueryRange(ctx, query, r)

	return v, w, nil
}

// Series finds series by label matchers.
func (n *IgnoreErrorAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	v, w, _ := n.API.Series(ctx, matches, startTime, endTime)

	return v, w, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (n *IgnoreErrorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	v, w, _ := n.API.GetValue(ctx, start, end, matchers)

	return v, w, nil
}

// Key returns a labelset used to determine other api clients that are the "same"
func (n *IgnoreErrorAPI) Key() model.LabelSet {
	if apiLabels, ok := n.API.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}
