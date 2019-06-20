package caching

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/jacksontj/promxy/pkg/promclient"
)

type FailureReportingAPI struct {
	promclient.API

	I int    //servergroup offset
	T string // target string
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (a *FailureReportingAPI) LabelNames(ctx context.Context) ([]string, api.Warnings, error) {
	v, w, err := a.API.LabelNames(ctx)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}

// LabelValues performs a query for the values of the given label.
func (a *FailureReportingAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	v, w, err := a.API.LabelValues(ctx, label)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}

// Query performs a query for the given time.
func (a *FailureReportingAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	v, w, err := a.API.Query(ctx, query, ts)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}

// QueryRange performs a query for the given range.
func (a *FailureReportingAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	v, w, err := a.API.QueryRange(ctx, query, r)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}

// Series finds series by label matchers.
func (a *FailureReportingAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	v, w, err := a.API.Series(ctx, matches, startTime, endTime)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *FailureReportingAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	v, w, err := a.API.GetValue(ctx, start, end, matchers)
	if err != nil {
		GetRequestContext(ctx).AddFailedTarget(a.I, a.T)
	}
	return v, w, err
}
