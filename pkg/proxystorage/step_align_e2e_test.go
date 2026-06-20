package proxystorage

import (
	"context"
	"strconv"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunks"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promclient"
)

// stepAlignStub mimics a step-aligning backend (Mimir/Cortex): its QueryRange
// snaps the request start down to the epoch grid and returns a dense series at
// every k*step in the window, regardless of the off-grid start it was asked
// for. value(t) = t/1000 (seconds) so re-stamped values are easy to assert.
type stepAlignStub struct{ stubAPI }

func (a *stepAlignStub) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	stepMs := r.Step.Milliseconds()
	if stepMs <= 0 {
		return storage.EmptySeriesSet()
	}
	startMs, endMs := r.Start.UnixMilli(), r.End.UnixMilli()
	first := (startMs / stepMs) * stepMs // floor to epoch grid
	var samples []chunks.Sample
	for t := first; t <= endMs; t += stepMs {
		samples = append(samples, promapi.FloatSample(t, float64(t)/1000))
	}
	s := promapi.NewSeries(labels.FromStrings(model.MetricNameLabel, "foo"), samples)
	return promapi.NewSeriesSet([]storage.Series{s}, nil, nil)
}

const (
	e2eStep   = int64(3600)       // 1h
	e2eGridT0 = int64(1781654400) // multiple of 3600
	e2eOffR   = int64(1800)       // off-grid by 30min (> 5m lookback)
	e2eN      = 6
)

func newProxyStorage(t *testing.T, client promclient.API) (*ProxyStorage, *promql.Engine) {
	t.Helper()
	ps := &ProxyStorage{}
	ps.state.Store(&proxyStorageState{
		client: client,
		cfg:    &proxyconfig.Config{},
	})
	eng := promql.NewEngine(promql.EngineOpts{
		MaxSamples:       1e6,
		Timeout:          10 * time.Second,
		LookbackDelta:    5 * time.Minute,
		EnableAtModifier: true,
	})
	eng.NodeReplacer = ps.NodeReplacer
	return ps, eng
}

func runRange(t *testing.T, ps *ProxyStorage, eng *promql.Engine, expr string, startSec, endSec int64) promql.Matrix {
	t.Helper()
	q, err := eng.NewRangeQuery(context.Background(), ps, nil, expr,
		time.Unix(startSec, 0), time.Unix(endSec, 0), time.Duration(e2eStep)*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	res := q.Exec(context.Background())
	if res.Err != nil {
		t.Fatal(res.Err)
	}
	m, err := res.Matrix()
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func countPoints(m promql.Matrix) int {
	n := 0
	for _, s := range m {
		n += len(s.Floats)
	}
	return n
}

// TestStepAlign_E2E_Pushdown drives the real proxystorage pushdown (NodeReplacer
// -> QueryRange -> StepAlignClient re-stamp -> engine re-eval) for an off-grid
// range query. With the wrapper the data is recovered on the requested grid;
// without it every point is dropped (issue #787).
func TestStepAlign_E2E_Pushdown(t *testing.T) {
	startSec, endSec := e2eGridT0+e2eOffR, e2eGridT0+e2eOffR+(e2eN-1)*e2eStep

	// With per-SG StepAlign wrapping.
	ps, eng := newProxyStorage(t, &promclient.StepAlignClient{API: &stepAlignStub{}})
	m := runRange(t, ps, eng, "foo", startSec, endSec)
	if got := countPoints(m); got != e2eN {
		t.Fatalf("with step_align: expected %d points, got %d", e2eN, got)
	}
	// Each output point sits on the requested grid; its value is the backend's
	// as-of-(T-r) sample (the documented skew), i.e. value == (T - r) seconds.
	for _, fp := range m[0].Floats {
		if fp.T%(e2eStep*1000) != e2eOffR*1000 {
			t.Errorf("point t=%d not on requested phase (want %d)", fp.T, e2eOffR*1000)
		}
		if want := float64(fp.T/1000 - e2eOffR); fp.F != want {
			t.Errorf("point t=%d: value=%v want %v", fp.T, fp.F, want)
		}
	}

	// Control: same backend, NOT wrapped -> the bug (no data).
	psRaw, engRaw := newProxyStorage(t, &stepAlignStub{})
	if got := countPoints(runRange(t, psRaw, engRaw, "foo", startSec, endSec)); got != 0 {
		t.Fatalf("control (no wrapper) expected 0 points (the bug), got %d", got)
	}
}

// TestStepAlign_E2E_Offset confirms an `offset` modifier still lines up: promxy
// pushes the range down at Start-offset, so the re-stamp reference frame is
// Start-offset's phase, which is exactly where the engine looks the samples back
// up (evalGrid - offset). Data must still be recovered.
func TestStepAlign_E2E_Offset(t *testing.T) {
	startSec, endSec := e2eGridT0+e2eOffR, e2eGridT0+e2eOffR+(e2eN-1)*e2eStep

	ps, eng := newProxyStorage(t, &promclient.StepAlignClient{API: &stepAlignStub{}})
	m := runRange(t, ps, eng, "foo offset 1h", startSec, endSec)
	if got := countPoints(m); got != e2eN {
		t.Fatalf("with step_align + offset: expected %d points, got %d", e2eN, got)
	}

	psRaw, engRaw := newProxyStorage(t, &stepAlignStub{})
	if got := countPoints(runRange(t, psRaw, engRaw, "foo offset 1h", startSec, endSec)); got != 0 {
		t.Fatalf("control (no wrapper) + offset expected 0 points, got %d", got)
	}
}

// TestStepAlign_E2E_AtModifier confirms the re-stamp does not break the @-pinned
// pushdown path: the result is step-invariant and still returns data (rather
// than the no-data the bug would produce). The pinned value is the backend's
// grid-snapped sample, which is the step-aligning backend's own behavior.
func TestStepAlign_E2E_AtModifier(t *testing.T) {
	startSec, endSec := e2eGridT0+e2eOffR, e2eGridT0+e2eOffR+(e2eN-1)*e2eStep

	ps, eng := newProxyStorage(t, &promclient.StepAlignClient{API: &stepAlignStub{}})
	// @ pinned off-grid -- the path most likely to interact with the re-stamp.
	m := runRange(t, ps, eng, "foo @ "+strconv.FormatInt(e2eGridT0+e2eOffR, 10), startSec, endSec)
	if got := countPoints(m); got != e2eN {
		t.Fatalf("with step_align + @: expected %d points, got %d", e2eN, got)
	}
	// step-invariant: every point carries the same (pinned) value.
	for _, fp := range m[0].Floats {
		if fp.F != m[0].Floats[0].F {
			t.Fatalf("@-pinned result not step-invariant: %v vs %v", fp.F, m[0].Floats[0].F)
		}
	}
}
