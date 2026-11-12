package promclient

import (
	"context"
	"errors"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// metricTestAPI returns err (which may be nil) from every call.
type metricTestAPI struct {
	err error
}

func (a *metricTestAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, a.err
}

func (a *metricTestAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, a.err
}

func (a *metricTestAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return a.seriesSet()
}

func (a *metricTestAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return a.seriesSet()
}

func (a *metricTestAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, a.err
}

func (a *metricTestAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return a.seriesSet()
}

func (a *metricTestAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, a.err
}

func (a *metricTestAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, a.err
}

func (a *metricTestAPI) seriesSet() storage.SeriesSet {
	if a.err != nil {
		return storage.ErrSeriesSet(a.err)
	}
	return storage.EmptySeriesSet()
}

type metricRecorder struct {
	calls []string
}

func (r *metricRecorder) record(api, status string, took float64) {
	r.calls = append(r.calls, api+"/"+status)
}

// callAll invokes every API method once, in a fixed order.
func callAll(api API, start, end time.Time) {
	ctx := context.TODO()
	api.LabelNames(ctx, nil, start, end)
	api.LabelValues(ctx, "__name__", nil, start, end)
	api.Query(ctx, "up", end).Err()
	api.QueryRange(ctx, "up", v1.Range{Start: start, End: end, Step: time.Minute}).Err()
	api.Series(ctx, []string{"up"}, start, end)
	api.GetValue(ctx, start, end, nil).Err()
	api.Metadata(ctx, "", "")
	api.QueryExemplars(ctx, "up", start, end)
}

var allCalls = []string{"label_names", "label_values", "query", "query_range", "series", "get_value", "metadata", "query_exemplars"}

func TestMetricAPIRecordsCalls(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		status string
	}{
		{name: "success", status: "success"},
		{name: "error", err: errors.New("downstream error"), status: "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &metricRecorder{}
			api := &MetricAPI{A: &metricTestAPI{err: tt.err}, MetricFunc: rec.record}

			end := time.Now()
			callAll(api, end.Add(-time.Hour), end)

			if len(rec.calls) != len(allCalls) {
				t.Fatalf("expected %d recorded calls, got %d: %v", len(allCalls), len(rec.calls), rec.calls)
			}
			for i, call := range allCalls {
				if want := call + "/" + tt.status; rec.calls[i] != want {
					t.Errorf("call %d: expected %q, got %q", i, want, rec.calls[i])
				}
			}
		})
	}
}

// Calls answered by a wrapper above MetricAPI without being forwarded must not
// be recorded.
func TestMetricAPIBeneathTimeFilter(t *testing.T) {
	now := time.Now()
	window := 24 * time.Hour

	tests := []struct {
		name       string
		start, end time.Time
		want       int
	}{
		{name: "in range", start: now.Add(-time.Hour), end: now, want: len(allCalls)},
		// Metadata has no time range, so it is the only call forwarded.
		{name: "out of range", start: now.Add(-3 * window), end: now.Add(-2 * window), want: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := &metricRecorder{}
			start := -window
			api := &RelativeTimeFilter{
				API:   &MetricAPI{A: &metricTestAPI{}, MetricFunc: rec.record},
				Start: &start,
			}

			callAll(api, tt.start, tt.end)

			if len(rec.calls) != tt.want {
				t.Fatalf("expected %d recorded calls, got %d: %v", tt.want, len(rec.calls), rec.calls)
			}
		})
	}
}
