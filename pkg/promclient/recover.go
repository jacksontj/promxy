package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// recoverAPI simply recovers all panics and returns them as errors
// this is used for testing
type recoverAPI struct{ A API }

// LabelNames returns all the unique label names present in the block in sorted order.
func (api *recoverAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) (v []string, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (api *recoverAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (v model.LabelValues, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (api *recoverAPI) Query(ctx context.Context, query string, ts time.Time) (v model.Value, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (api *recoverAPI) QueryRange(ctx context.Context, query string, r v1.Range) (v model.Value, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (api *recoverAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) (v []model.LabelSet, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (api *recoverAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (v model.Value, w v1.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (api *recoverAPI) Metadata(ctx context.Context, metric, limit string) (v map[string][]v1.Metadata, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.A.Metadata(ctx, metric, limit)
}
