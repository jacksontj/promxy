package promclient

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

// AbsoluteTimeFilter will filter queries out (return nil,nil) for all queries outside the given times
type AbsoluteTimeFilter struct {
	API
	Start, End time.Time
}

// Query performs a query for the given time.
func (tf *AbsoluteTimeFilter) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	if (!tf.Start.IsZero() && ts.Before(tf.Start)) || (!tf.End.IsZero() && ts.After(tf.End)) {
		return nil, nil, nil
	}

	return tf.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (tf *AbsoluteTimeFilter) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	if (!tf.Start.IsZero() && r.End.Before(tf.Start)) || (!tf.End.IsZero() && r.Start.After(tf.End)) {
		return nil, nil, nil
	}

	return tf.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (tf *AbsoluteTimeFilter) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	if (!tf.Start.IsZero() && endTime.Before(tf.Start)) || (!tf.End.IsZero() && startTime.After(tf.End)) {
		return nil, nil, nil
	}
	return tf.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (tf *AbsoluteTimeFilter) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	if (!tf.Start.IsZero() && end.Before(tf.Start)) || (!tf.End.IsZero() && start.After(tf.End)) {
		return nil, nil, nil
	}

	return tf.API.GetValue(ctx, start, end, matchers)
}

// RelativeTimeFilter will filter queries out (return nil,nil) for all queries outside the given durations relative to time.Now()
type RelativeTimeFilter struct {
	API
	Start, End *time.Duration
}

func (tf *RelativeTimeFilter) window() (time.Time, time.Time) {
	now := time.Now()

	var start, end time.Time
	if tf.Start != nil {
		start = now.Add(*tf.Start)
	}
	if tf.End != nil {
		end = now.Add(*tf.End)
	}

	return start, end
}

// Query performs a query for the given time.
func (tf *RelativeTimeFilter) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && ts.Before(tfStart)) || (!tfEnd.IsZero() && ts.After(tfEnd)) {
		return nil, nil, nil
	}

	return tf.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (tf *RelativeTimeFilter) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && r.End.Before(tfStart)) || (!tfEnd.IsZero() && r.Start.After(tfEnd)) {
		return nil, nil, nil
	}

	return tf.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (tf *RelativeTimeFilter) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && endTime.Before(tfStart)) || (!tfEnd.IsZero() && startTime.After(tfEnd)) {
		return nil, nil, nil
	}
	return tf.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (tf *RelativeTimeFilter) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && end.Before(tfStart)) || (!tfEnd.IsZero() && start.After(tfEnd)) {
		return nil, nil, nil
	}

	return tf.API.GetValue(ctx, start, end, matchers)
}
