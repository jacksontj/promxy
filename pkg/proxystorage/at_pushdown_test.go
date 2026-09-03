package proxystorage

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// atCountingStub counts the downstream requests a pushdown issues, and how many
// samples per series the range requests would ship back. Both call shapes
// return the same single series so the engine can evaluate either one.
type atCountingStub struct {
	stubAPI

	mu         sync.Mutex
	instant    int
	rangeCalls int
	rangeSteps int
}

func (a *atCountingStub) series(samples []chunks.Sample) storage.SeriesSet {
	s := promapi.NewSeries(
		labels.FromStrings(model.MetricNameLabel, "foo", "job", "j"),
		samples,
	)
	return promapi.NewSeriesSet([]storage.Series{s}, nil, nil)
}

func (a *atCountingStub) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	a.mu.Lock()
	a.instant++
	a.mu.Unlock()
	return a.series([]chunks.Sample{promapi.FloatSample(ts.UnixMilli(), 42)})
}

func (a *atCountingStub) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	stepMs := r.Step.Milliseconds()
	if stepMs <= 0 {
		return storage.EmptySeriesSet()
	}
	var samples []chunks.Sample
	for t := r.Start.UnixMilli(); t <= r.End.UnixMilli(); t += stepMs {
		samples = append(samples, promapi.FloatSample(t, 42))
	}
	a.mu.Lock()
	a.rangeCalls++
	a.rangeSteps += len(samples)
	a.mu.Unlock()
	return a.series(samples)
}

func (a *atCountingStub) counts() (instant, rangeCalls, rangeSteps int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.instant, a.rangeCalls, a.rangeSteps
}

// TestAtPushdownIssuesOneInstantQuery covers every pushdown site that can carry
// an @-pinned subtree. The result of such a subtree is identical at every step,
// so it is fetched once and replicated rather than making the downstream
// evaluate the same instant per step and ship back an identical sample each
// time. Previously only the Call site did this; the rest inlined a plain
// QueryRange.
func TestAtPushdownIssuesOneInstantQuery(t *testing.T) {
	at := e2eGridT0
	atStr := fmt.Sprintf("%d", at)

	tests := []struct {
		name string
		expr string
	}{
		{name: "AggregateExpr sum", expr: "sum(foo @ " + atStr + ")"},
		{name: "AggregateExpr count", expr: "count(foo @ " + atStr + ")"},
		{name: "AggregateExpr count_values", expr: `count_values("l", foo @ ` + atStr + `)`},
		{name: "AggregateExpr topk", expr: "topk(1, foo @ " + atStr + ")"},
		{name: "VectorSelector", expr: "foo @ " + atStr},
		{name: "BinaryExpr with a vector selector", expr: "foo @ " + atStr + " > 1"},
		{name: "BinaryExpr with an aggregate", expr: "min(foo @ " + atStr + ") > 1"},
		{name: "Call", expr: "sort(foo @ " + atStr + ")"},
	}

	startSec, endSec := e2eGridT0, e2eGridT0+(e2eN-1)*e2eStep

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &atCountingStub{}
			ps, eng := newProxyStorage(t, stub)

			runRange(t, ps, eng, tt.expr, startSec, endSec)

			instant, rangeCalls, rangeSteps := stub.counts()
			if rangeCalls != 0 {
				t.Errorf("issued %d range queries (%d samples/series), want 0 -- the @-pinned result is step-invariant", rangeCalls, rangeSteps)
			}
			if instant != 1 {
				t.Errorf("issued %d instant queries, want 1", instant)
			}
		})
	}
}

// TestPushdownWithoutAtUsesRangeQuery is the control for the above: with no @
// in the subtree the result varies per step, so it still has to be a range
// query.
func TestPushdownWithoutAtUsesRangeQuery(t *testing.T) {
	tests := []struct {
		name string
		expr string
	}{
		{name: "AggregateExpr sum", expr: "sum(foo)"},
		{name: "AggregateExpr count", expr: "count(foo)"},
		{name: "AggregateExpr count_values", expr: `count_values("l", foo)`},
		{name: "AggregateExpr topk", expr: "topk(1, foo)"},
		{name: "VectorSelector", expr: "foo"},
		{name: "BinaryExpr with a vector selector", expr: "foo > 1"},
		{name: "BinaryExpr with an aggregate", expr: "min(foo) > 1"},
		{name: "Call", expr: "sort(foo)"},
	}

	startSec, endSec := e2eGridT0, e2eGridT0+(e2eN-1)*e2eStep

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &atCountingStub{}
			ps, eng := newProxyStorage(t, stub)

			runRange(t, ps, eng, tt.expr, startSec, endSec)

			instant, rangeCalls, _ := stub.counts()
			if rangeCalls != 1 {
				t.Errorf("issued %d range queries, want 1", rangeCalls)
			}
			if instant != 0 {
				t.Errorf("issued %d instant queries, want 0", instant)
			}
		})
	}
}
