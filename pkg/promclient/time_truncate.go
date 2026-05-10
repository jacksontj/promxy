package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// NewTimeTruncate returns a new TimeTruncate client -- which will truncate the time to the millisecond.
func NewTimeTruncate(a API) *TimeTruncate {
	return &TimeTruncate{a}
}

const truncateDuration = time.Millisecond

// TimeTruncate is a workaround to https://github.com/jacksontj/promxy/issues/212
// context: https://github.com/prometheus/prometheus/issues/5972
// For now we need to truncate the time so that prometheus doesn't round up and return no data <= the timestamp
// we requested
type TimeTruncate struct {
	API
}

// Query performs a query for the given time.
func (t *TimeTruncate) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return t.API.Query(ctx, query, ts.Truncate(truncateDuration))
}

// QueryRange performs a query for the given range.
func (t *TimeTruncate) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return t.API.QueryRange(ctx, query, v1.Range{
		Start: r.Start.Truncate(truncateDuration),
		End:   r.End.Truncate(truncateDuration),
		Step:  r.Step,
	})
}

// Series finds series by label matchers.
func (t *TimeTruncate) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return t.API.Series(ctx, matches, startTime.Truncate(truncateDuration), endTime.Truncate(truncateDuration))
}

// GetValue loads the raw data for a given set of matchers in the time range
func (t *TimeTruncate) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return t.API.GetValue(ctx, start.Truncate(truncateDuration), end.Truncate(truncateDuration), matchers)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (t *TimeTruncate) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return t.API.QueryExemplars(ctx, query, startTime.Truncate(truncateDuration), endTime.Truncate(truncateDuration))
}
