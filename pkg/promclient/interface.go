package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// API Subset of the interface defined in the prometheus client
type API interface {
	// LabelNames returns all the unique label names present in the block in sorted order.
	LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error)
	// LabelValues performs a query for the values of the given label.
	LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error)
	// Query performs a query for the given time.
	Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error)
	// QueryRange performs a query for the given range.
	QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error)
	// Series finds series by label matchers.
	Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error)
	// GetValue loads the raw data for a given set of matchers in the time range
	GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error)
	// Metadata returns metadata about metrics currently scraped by the metric name.
	Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error)
}

// APILabels includes a Key() mechanism to differentiate which APIs are "the same"
type APILabels interface {
	API
	// Key returns a labelset used to determine other api clients that are the "same"
	Key() model.LabelSet
}
