package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// AbsoluteTimeFilter will filter queries out (return nil,nil) for all queries outside the given times
type AbsoluteTimeFilter struct {
	API
	Start, End time.Time
	Truncate   bool
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (tf *AbsoluteTimeFilter) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	if (!tf.Start.IsZero() && endTime.Before(tf.Start)) || (!tf.End.IsZero() && startTime.After(tf.End)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if startTime.Before(tf.Start) {
			startTime = tf.Start
		}
		if endTime.After(tf.End) {
			endTime = tf.End
		}
	}

	return tf.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (tf *AbsoluteTimeFilter) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if (!tf.Start.IsZero() && endTime.Before(tf.Start)) || (!tf.End.IsZero() && startTime.After(tf.End)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tf.Start.IsZero() && startTime.Before(tf.Start) {
			startTime = tf.Start
		}
		if !tf.End.IsZero() && endTime.After(tf.End) {
			endTime = tf.End
		}
	}

	return tf.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (tf *AbsoluteTimeFilter) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	if (!tf.Start.IsZero() && ts.Before(tf.Start)) || (!tf.End.IsZero() && ts.After(tf.End)) {
		return nil, nil, nil
	}

	return tf.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (tf *AbsoluteTimeFilter) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	if (!tf.Start.IsZero() && r.End.Before(tf.Start)) || (!tf.End.IsZero() && r.Start.After(tf.End)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tf.Start.IsZero() && r.Start.Before(tf.Start) {
			remainder := tf.Start.Sub(r.Start) % r.Step
			if remainder > 0 {
				r.Start = tf.Start.Add(r.Step - remainder)
			} else {
				r.Start = tf.Start
			}
		}
		if !tf.End.IsZero() && r.End.After(tf.End) {
			r.End = tf.End
		}
	}

	return tf.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (tf *AbsoluteTimeFilter) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if (!tf.Start.IsZero() && endTime.Before(tf.Start)) || (!tf.End.IsZero() && startTime.After(tf.End)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tf.Start.IsZero() && startTime.Before(tf.Start) {
			startTime = tf.Start
		}
		if !tf.End.IsZero() && endTime.After(tf.End) {
			endTime = tf.End
		}
	}

	return tf.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (tf *AbsoluteTimeFilter) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	if (!tf.Start.IsZero() && end.Before(tf.Start)) || (!tf.End.IsZero() && start.After(tf.End)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tf.Start.IsZero() && start.Before(tf.Start) {
			start = tf.Start
		}
		if !tf.End.IsZero() && end.After(tf.End) {
			end = tf.End
		}
	}

	return tf.API.GetValue(ctx, start, end, matchers)
}

// RelativeTimeFilter will filter queries out (return nil,nil) for all queries outside the given durations relative to time.Now()
type RelativeTimeFilter struct {
	API
	Start, End *time.Duration
	Truncate   bool
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

// LabelNames returns all the unique label names present in the block in sorted order.
func (tf *RelativeTimeFilter) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && endTime.Before(tfStart)) || (!tfEnd.IsZero() && startTime.After(tfEnd)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tfStart.IsZero() && startTime.Before(tfStart) {
			startTime = tfStart
		}
		if !tfEnd.IsZero() && endTime.After(tfEnd) {
			endTime = tfEnd
		}
	}

	return tf.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (tf *RelativeTimeFilter) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && endTime.Before(tfStart)) || (!tfEnd.IsZero() && startTime.After(tfEnd)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tfStart.IsZero() && startTime.Before(tfStart) {
			startTime = tfStart
		}
		if !tfEnd.IsZero() && endTime.After(tfEnd) {
			endTime = tfEnd
		}
	}

	return tf.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (tf *RelativeTimeFilter) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && ts.Before(tfStart)) || (!tfEnd.IsZero() && ts.After(tfEnd)) {
		return nil, nil, nil
	}

	return tf.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (tf *RelativeTimeFilter) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && r.End.Before(tfStart)) || (!tfEnd.IsZero() && r.Start.After(tfEnd)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tfStart.IsZero() && r.Start.Before(tfStart) {
			remainder := tfStart.Sub(r.Start) % r.Step
			if remainder > 0 {
				r.Start = tfStart.Add(r.Step - remainder)
			} else {
				r.Start = tfStart
			}
		}
		if !tfEnd.IsZero() && r.End.After(tfEnd) {
			r.End = tfEnd
		}
	}

	return tf.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (tf *RelativeTimeFilter) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && endTime.Before(tfStart)) || (!tfEnd.IsZero() && startTime.After(tfEnd)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tfStart.IsZero() && startTime.Before(tfStart) {
			startTime = tfStart
		}
		if !tfEnd.IsZero() && endTime.Before(tfEnd) {
			endTime = tfEnd
		}
	}

	return tf.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (tf *RelativeTimeFilter) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	tfStart, tfEnd := tf.window()
	if (!tfStart.IsZero() && end.Before(tfStart)) || (!tfEnd.IsZero() && start.After(tfEnd)) {
		return nil, nil, nil
	}

	if tf.Truncate {
		if !tfStart.IsZero() && start.Before(tfStart) {
			start = tfStart
		}
		if !tfEnd.IsZero() && end.Before(tfEnd) {
			end = tfEnd
		}
	}

	return tf.API.GetValue(ctx, start, end, matchers)
}
