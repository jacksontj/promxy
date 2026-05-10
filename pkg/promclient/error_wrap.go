package promclient

import (
	"context"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

func (e *ErrorWrap) wrap(err error) error {
	if err != nil {
		return errors.Wrap(err, e.Msg)
	}
	return nil
}

type ErrorWrap struct {
	A   API
	Msg string
}

func (e *ErrorWrap) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) (v []string, w v1.Warnings, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, e.Msg)
		}
	}()
	return e.A.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (e *ErrorWrap) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (v model.LabelValues, w v1.Warnings, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, e.Msg)
		}
	}()
	return e.A.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (e *ErrorWrap) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return MapErrSeriesSet(e.A.Query(ctx, query, ts), e.wrap)
}

// QueryRange performs a query for the given range.
func (e *ErrorWrap) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return MapErrSeriesSet(e.A.QueryRange(ctx, query, r), e.wrap)
}

// Series finds series by label matchers.
func (e *ErrorWrap) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) (v []model.LabelSet, w v1.Warnings, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, e.Msg)
		}
	}()
	return e.A.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (e *ErrorWrap) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return MapErrSeriesSet(e.A.GetValue(ctx, start, end, matchers), e.wrap)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (e *ErrorWrap) Metadata(ctx context.Context, metric, limit string) (v map[string][]v1.Metadata, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, e.Msg)
		}
	}()
	return e.A.Metadata(ctx, metric, limit)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (e *ErrorWrap) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) (v []v1.ExemplarQueryResult, err error) {
	defer func() {
		if err != nil {
			err = errors.Wrap(err, e.Msg)
		}
	}()
	return e.A.QueryExemplars(ctx, query, startTime, endTime)
}
