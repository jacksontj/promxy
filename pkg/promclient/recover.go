package promclient

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// recoverAPI simply recovers all panics and returns them as errors
// this is used for testing
type recoverAPI struct{ API }

// LabelValues performs a query for the values of the given label.
func (api *recoverAPI) LabelValues(ctx context.Context, label string) (v model.LabelValues, w api.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.API.LabelValues(ctx, label)
}

// Query performs a query for the given time.
func (api *recoverAPI) Query(ctx context.Context, query string, ts time.Time) (v model.Value, w api.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (api *recoverAPI) QueryRange(ctx context.Context, query string, r v1.Range) (v model.Value, w api.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (api *recoverAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) (v []model.LabelSet, w api.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (api *recoverAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (v model.Value, w api.Warnings, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = r.(error)
		}
	}()
	return api.API.GetValue(ctx, start, end, matchers)
}
