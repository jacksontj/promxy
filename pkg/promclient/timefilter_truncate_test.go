package promclient

import (
	"context"
	"sort"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// windowRecorder captures the [start, end] range each call was forwarded with, so
// these tests can assert on what the filter actually sent downstream rather
// than only on whether it forwarded at all (which is what the stubAPI-based
// tests in timefilter_test.go cover).
type windowRecorder struct {
	API
	called     bool
	start, end time.Time
}

func (r *windowRecorder) LabelNames(_ context.Context, _ []string, s, e time.Time) ([]string, v1.Warnings, error) {
	r.called, r.start, r.end = true, s, e
	return nil, nil, nil
}

func (r *windowRecorder) LabelValues(_ context.Context, _ string, _ []string, s, e time.Time) (model.LabelValues, v1.Warnings, error) {
	r.called, r.start, r.end = true, s, e
	return nil, nil, nil
}

func (r *windowRecorder) Series(_ context.Context, _ []string, s, e time.Time) ([]model.LabelSet, v1.Warnings, error) {
	r.called, r.start, r.end = true, s, e
	return nil, nil, nil
}

func (r *windowRecorder) GetValue(_ context.Context, s, e time.Time, _ []*labels.Matcher) storage.SeriesSet {
	r.called, r.start, r.end = true, s, e
	return storage.EmptySeriesSet()
}

func (r *windowRecorder) QueryExemplars(_ context.Context, _ string, s, e time.Time) ([]v1.ExemplarQueryResult, error) {
	r.called, r.start, r.end = true, s, e
	return nil, nil
}

// rangeCalls are the API methods that take a [start, end] range and truncate it
// against the filter window. QueryRange is excluded: it truncates too, but also
// has to keep the result on the requested step grid, so it is covered by
// Test{Absolute,Relative}TimeFilterStepAlignment instead.
var rangeCalls = map[string]func(API, time.Time, time.Time){
	"LabelNames":     func(a API, s, e time.Time) { a.LabelNames(context.TODO(), nil, s, e) },
	"LabelValues":    func(a API, s, e time.Time) { a.LabelValues(context.TODO(), "__name__", nil, s, e) },
	"Series":         func(a API, s, e time.Time) { a.Series(context.TODO(), nil, s, e) },
	"GetValue":       func(a API, s, e time.Time) { a.GetValue(context.TODO(), s, e, nil) },
	"QueryExemplars": func(a API, s, e time.Time) { a.QueryExemplars(context.TODO(), "foo", s, e) },
}

// forEachRangeCall runs check against every range-taking method, so a fix (or a
// regression) in one method can't hide behind the others.
func forEachRangeCall(t *testing.T, mk func(API) API, reqStart, reqEnd time.Time, check func(*testing.T, *windowRecorder)) {
	t.Helper()
	names := make([]string, 0, len(rangeCalls))
	for name := range rangeCalls {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			rec := &windowRecorder{}
			rangeCalls[name](mk(rec), reqStart, reqEnd)
			if !rec.called {
				t.Fatal("call was not forwarded downstream")
			}
			check(t, rec)
		})
	}
}

// Truncation may only ever narrow the requested range. Pushing the end
// *forward* to the window edge sends the downstream a wider request than was
// asked for: it over-fetches (GetValue is the raw-sample path) and it changes
// results, since Series then reports series that exist only in the extra
// window.
func TestRelativeTimeFilterTruncateNeverWidens(t *testing.T) {
	// A long-term-storage shaped group: holds data from 90d ago to 12h ago.
	start := -90 * 24 * time.Hour
	end := -12 * time.Hour

	// A request that sits wholly inside that window; nothing to adjust.
	reqStart := time.Now().Add(-25 * time.Hour)
	reqEnd := time.Now().Add(-24 * time.Hour)

	forEachRangeCall(t,
		func(a API) API { return &RelativeTimeFilter{API: a, Start: &start, End: &end, Truncate: true} },
		reqStart, reqEnd,
		func(t *testing.T, rec *windowRecorder) {
			if rec.start.Before(reqStart) {
				t.Errorf("start widened: requested %v, sent %v", reqStart, rec.start)
			}
			if rec.end.After(reqEnd) {
				t.Errorf("end widened: requested %v, sent %v", reqEnd, rec.end)
			}
		},
	)
}

// ...and it must still clamp a request that overruns the window.
func TestRelativeTimeFilterTruncateClamps(t *testing.T) {
	start := -2 * time.Hour
	end := -1 * time.Hour

	reqStart := time.Now().Add(-24 * time.Hour)
	reqEnd := time.Now()

	// The window is recomputed from time.Now() inside the filter, so allow a
	// minute of slack on either side rather than asserting an exact instant.
	const slack = time.Minute

	forEachRangeCall(t,
		func(a API) API { return &RelativeTimeFilter{API: a, Start: &start, End: &end, Truncate: true} },
		reqStart, reqEnd,
		func(t *testing.T, rec *windowRecorder) {
			if rec.start.Before(time.Now().Add(start).Add(-slack)) {
				t.Errorf("start not clamped to the window: sent %v", rec.start)
			}
			if rec.end.After(time.Now().Add(end).Add(slack)) {
				t.Errorf("end not clamped to the window: sent %v", rec.end)
			}
		},
	)
}

// An unset window edge means unbounded, so it must be left alone rather than
// used as a bound. Truncating against a zero time.Time collapses the request to
// year 1, and the server group then silently answers nothing.
func TestAbsoluteTimeFilterTruncateUnsetEdge(t *testing.T) {
	reqStart := time.Now().Add(-time.Hour)
	reqEnd := time.Now()

	t.Run("end unset", func(t *testing.T) {
		filterStart := time.Now().Add(-24 * time.Hour)
		forEachRangeCall(t,
			func(a API) API { return &AbsoluteTimeFilter{API: a, Start: filterStart, Truncate: true} },
			reqStart, reqEnd,
			func(t *testing.T, rec *windowRecorder) {
				if !rec.end.Equal(reqEnd) {
					t.Errorf("end rewritten against an unset filter end: requested %v, sent %v", reqEnd, rec.end)
				}
			},
		)
	})

	t.Run("start unset", func(t *testing.T) {
		filterEnd := time.Now().Add(time.Hour)
		forEachRangeCall(t,
			func(a API) API { return &AbsoluteTimeFilter{API: a, End: filterEnd, Truncate: true} },
			reqStart, reqEnd,
			func(t *testing.T, rec *windowRecorder) {
				if !rec.start.Equal(reqStart) {
					t.Errorf("start rewritten against an unset filter start: requested %v, sent %v", reqStart, rec.start)
				}
			},
		)
	})
}

func TestRelativeTimeFilterTruncateUnsetEdge(t *testing.T) {
	start := -24 * time.Hour
	reqStart := time.Now().Add(-time.Hour)
	reqEnd := time.Now()

	forEachRangeCall(t,
		func(a API) API { return &RelativeTimeFilter{API: a, Start: &start, Truncate: true} },
		reqStart, reqEnd,
		func(t *testing.T, rec *windowRecorder) {
			if !rec.end.Equal(reqEnd) {
				t.Errorf("end rewritten against an unset filter end: requested %v, sent %v", reqEnd, rec.end)
			}
		},
	)
}
