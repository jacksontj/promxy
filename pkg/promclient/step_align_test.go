package promclient

import (
	"context"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
)

const (
	stepSec = int64(3600)       // 1h step
	stepMs  = stepSec * 1000    //
	gridT0  = int64(1781654400) // multiple of 3600 -> epoch grid anchor (sec)
	offR    = int64(1800)       // off-grid by 30min (deliberately > 5m lookback)
	npts    = 6
)

// gridSeries builds one series with n float samples starting at startMs, spaced
// stepMs apart. value(t) = t/1000 so the as-of skew is human-readable in logs.
func gridSeries(lbls labels.Labels, startMs, n, stepMs int64) storage.Series {
	samples := make([]chunks.Sample, 0, n)
	for i := int64(0); i < n; i++ {
		t := startMs + i*stepMs
		samples = append(samples, promapi.FloatSample(t, float64(t)/1000))
	}
	return promapi.NewSeries(lbls, samples)
}

// epochGrid: samples at k*step -- what a step-aligning backend (Mimir/Cortex) returns.
func epochGrid(lbls labels.Labels) storage.Series {
	return gridSeries(lbls, gridT0*1000, npts, stepMs)
}

// reqGrid: samples at start+j*step -- what a non-aligning backend (vanilla
// Prometheus) returns for the same off-grid request.
func reqGrid(lbls labels.Labels) storage.Series {
	return gridSeries(lbls, (gridT0+offR)*1000, npts, stepMs)
}

// stubBackend is an API that returns a fixed set of series for QueryRange.
type stubBackend struct{ series []storage.Series }

func (s *stubBackend) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return promapi.NewSeriesSet(s.series, nil, nil)
}
func (s *stubBackend) Query(context.Context, string, time.Time) storage.SeriesSet {
	return storage.EmptySeriesSet()
}
func (s *stubBackend) LabelNames(context.Context, []string, time.Time, time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}
func (s *stubBackend) LabelValues(context.Context, string, []string, time.Time, time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}
func (s *stubBackend) Series(context.Context, []string, time.Time, time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}
func (s *stubBackend) GetValue(context.Context, time.Time, time.Time, []*labels.Matcher) storage.SeriesSet {
	return storage.EmptySeriesSet()
}
func (s *stubBackend) Metadata(context.Context, string, string) (map[string][]v1.Metadata, error) {
	return nil, nil
}
func (s *stubBackend) QueryExemplars(context.Context, string, time.Time, time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}

// dumpFloats returns the (t,v) pairs of the first series in ss.
func dumpFloats(ss storage.SeriesSet) (ts []int64, vs []float64) {
	if !ss.Next() {
		return nil, nil
	}
	it := ss.At().Iterator(nil)
	for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
		if vt == chunkenc.ValFloat {
			t, v := it.At()
			ts = append(ts, t)
			vs = append(vs, v)
		}
	}
	return ts, vs
}

func offGridRange() v1.Range {
	return v1.Range{
		Start: time.Unix(gridT0+offR, 0),
		End:   time.Unix(gridT0+offR+npts*stepSec, 0),
		Step:  time.Duration(stepSec) * time.Second,
	}
}

// histGridSeries builds a native-(float-)histogram series on the given grid.
// The histogram's Sum is set to its (pre-shift) timestamp so the test can
// confirm the payload is carried through unchanged while only the timestamp
// moves.
func histGridSeries(lbls labels.Labels, startMs, n, stepMs int64) storage.Series {
	samples := make([]chunks.Sample, 0, n)
	for i := int64(0); i < n; i++ {
		t := startMs + i*stepMs
		samples = append(samples, promapi.HistogramSample(t, &histogram.FloatHistogram{
			Count: float64(t),
			Sum:   float64(t),
		}))
	}
	return promapi.NewSeries(lbls, samples)
}

// dumpHist returns the (t, sum) pairs of the first series in ss.
func dumpHist(ss storage.SeriesSet) (ts []int64, sums []float64) {
	if !ss.Next() {
		return nil, nil
	}
	it := ss.At().Iterator(nil)
	for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
		if vt == chunkenc.ValHistogram || vt == chunkenc.ValFloatHistogram {
			t, fh := it.AtFloatHistogram(nil)
			ts = append(ts, t)
			sums = append(sums, fh.Sum)
		}
	}
	return ts, sums
}

// TestStepAlignClient_Restamp_Histogram: native histogram samples are shifted
// onto the requested grid (via AtFloatHistogram), payload untouched.
func TestStepAlignClient_Restamp_Histogram(t *testing.T) {
	client := &StepAlignClient{API: &stubBackend{series: []storage.Series{
		histGridSeries(labels.FromStrings("__name__", "foo"), gridT0*1000, npts, stepMs),
	}}}

	gotTs, gotSums := dumpHist(client.QueryRange(context.Background(), "foo", offGridRange()))
	if len(gotTs) != npts {
		t.Fatalf("expected %d histogram samples, got %d", npts, len(gotTs))
	}
	for i := range gotTs {
		wantT := (gridT0 + offR + int64(i)*stepSec) * 1000
		if gotTs[i] != wantT {
			t.Fatalf("hist sample %d: got t=%d want t=%d", i, gotTs[i], wantT)
		}
		if gotTs[i]%stepMs != offR*1000 {
			t.Fatalf("hist sample %d off requested phase: t=%d", i, gotTs[i])
		}
		// payload carried through unchanged: Sum is still the pre-shift timestamp.
		if wantSum := float64(wantT - offR*1000); gotSums[i] != wantSum {
			t.Fatalf("hist sample %d: Sum=%v want %v (payload must not change)", i, gotSums[i], wantSum)
		}
	}
}

// TestStepAlignClient_Restamp: epoch-grid samples are shifted onto the requested grid.
func TestStepAlignClient_Restamp(t *testing.T) {
	client := &StepAlignClient{API: &stubBackend{series: []storage.Series{
		epochGrid(labels.FromStrings("__name__", "foo")),
	}}}

	gotTs, _ := dumpFloats(client.QueryRange(context.Background(), "foo", offGridRange()))
	if len(gotTs) != npts {
		t.Fatalf("expected %d samples, got %d", npts, len(gotTs))
	}
	for i, got := range gotTs {
		want := (gridT0 + offR + int64(i)*stepSec) * 1000
		if got != want {
			t.Fatalf("sample %d: got t=%d want t=%d", i, got, want)
		}
		if got%stepMs != offR*1000 {
			t.Fatalf("sample %d off requested phase: t=%d phase=%d want %d", i, got, got%stepMs, offR*1000)
		}
	}
}

// TestStepAlignClient_NoOp: on-grid requests and non-positive steps are passed
// through untouched (same SeriesSet, no shift).
func TestStepAlignClient_NoOp(t *testing.T) {
	for _, tc := range []struct {
		name string
		r    v1.Range
	}{
		{"on-grid start", v1.Range{Start: time.Unix(gridT0, 0), End: time.Unix(gridT0+npts*stepSec, 0), Step: time.Duration(stepSec) * time.Second}},
		{"zero step", v1.Range{Start: time.Unix(gridT0+offR, 0), End: time.Unix(gridT0+offR, 0), Step: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &StepAlignClient{API: &stubBackend{series: []storage.Series{
				epochGrid(labels.FromStrings("__name__", "foo")),
			}}}
			gotTs, _ := dumpFloats(client.QueryRange(context.Background(), "foo", tc.r))
			for i, got := range gotTs {
				want := (gridT0 + int64(i)*stepSec) * 1000
				if got != want {
					t.Fatalf("sample %d mutated: got t=%d want t=%d", i, got, want)
				}
			}
		})
	}
}

// mergedQueryable mimics promxy fanning a leaf query_range out to two server
// groups and merging the result: source a ("Mimir", epoch grid) behind a
// StepAlignClient, source b ("vanilla Prom", request grid) untouched.
type mergedQueryable struct {
	a, b API
	r    v1.Range
}

func (q *mergedQueryable) Querier(mint, maxt int64) (storage.Querier, error) {
	return &mergedQuerier{q: q}, nil
}

type mergedQuerier struct{ q *mergedQueryable }

func (qq *mergedQuerier) Select(_ context.Context, _ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.SeriesSet {
	a := qq.q.a.QueryRange(context.Background(), "foo", qq.q.r)
	b := qq.q.b.QueryRange(context.Background(), "foo", qq.q.r)
	return storage.NewMergeSeriesSet([]storage.SeriesSet{a, b}, 0, storage.ChainedSeriesMerge)
}
func (qq *mergedQuerier) LabelValues(context.Context, string, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (qq *mergedQuerier) LabelNames(context.Context, *storage.LabelHints, ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (qq *mergedQuerier) Close() error { return nil }

func runRangeEngine(t *testing.T, qa storage.Queryable) int {
	t.Helper()
	eng := promql.NewEngine(promql.EngineOpts{MaxSamples: 1e6, Timeout: 10 * time.Second, LookbackDelta: 5 * time.Minute})
	q, err := eng.NewRangeQuery(context.Background(), qa, nil, "foo",
		time.Unix(gridT0+offR, 0), time.Unix(gridT0+offR+(npts-1)*stepSec, 0), time.Duration(stepSec)*time.Second)
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
	n := 0
	for _, s := range m {
		n += len(s.Floats)
	}
	return n
}

// TestStepAlignClient_MixedMerge_Disjoint: a step-aligning SG (wrapped) and a
// non-aligning SG (untouched) hold disjoint series. After re-stamp both land on
// the requested grid, so an off-grid range query (5m lookback) recovers every
// point from both -- whereas without the wrapper the aligned SG's points are
// all dropped (issue #787).
func TestStepAlignClient_MixedMerge_Disjoint(t *testing.T) {
	mimirSeries := func() storage.Series { return epochGrid(labels.FromStrings("__name__", "foo", "sg", "a")) }
	vanilla := &stubBackend{series: []storage.Series{reqGrid(labels.FromStrings("__name__", "foo", "sg", "b"))}}
	r := offGridRange()

	withAlign := runRangeEngine(t, &mergedQueryable{
		a: &StepAlignClient{API: &stubBackend{series: []storage.Series{mimirSeries()}}},
		b: vanilla,
		r: r,
	})
	if withAlign != 2*npts {
		t.Errorf("with step_align: expected both series whole (%d points), got %d", 2*npts, withAlign)
	}

	// Control: same topology, Mimir SG NOT wrapped -> its series is lost.
	noAlign := runRangeEngine(t, &mergedQueryable{
		a: &stubBackend{series: []storage.Series{mimirSeries()}},
		b: vanilla,
		r: r,
	})
	if noAlign >= withAlign {
		t.Errorf("control should lose the aligned SG's points; got noAlign=%d withAlign=%d", noAlign, withAlign)
	}
	t.Logf("disjoint mixed merge: with step_align=%d points, without=%d (per-series=%d, 2 series)", withAlign, noAlign, npts)
}

// TestStepAlignClient_SameSeriesSkew documents the one wrinkle: when the SAME
// series is co-served by an aligning and a non-aligning backend, timestamps
// coincide after re-stamp (no "T vs T-step"), but the aligned value at T is the
// backend's value as-of T-r. Harmless for disjoint/sharded series.
func TestStepAlignClient_SameSeriesSkew(t *testing.T) {
	aligned := &StepAlignClient{API: &stubBackend{series: []storage.Series{epochGrid(labels.FromStrings("__name__", "foo"))}}}
	native := &stubBackend{series: []storage.Series{reqGrid(labels.FromStrings("__name__", "foo"))}}

	aTs, aVs := dumpFloats(aligned.QueryRange(context.Background(), "foo", offGridRange()))
	bTs, bVs := dumpFloats(native.QueryRange(context.Background(), "foo", offGridRange()))

	for i := range aTs {
		if aTs[i] != bTs[i] {
			t.Fatalf("timestamps diverge at %d: aligned=%d native=%d", i, aTs[i], bTs[i])
		}
		if skew := bVs[i] - aVs[i]; skew != float64(offR) {
			t.Errorf("point %d: expected as-of skew of %ds (= r), got %.0f", i, offR, skew)
		}
		t.Logf("t=%d aligned=%.0f native=%.0f skew=%.0fs", aTs[i]/1000, aVs[i], bVs[i], bVs[i]-aVs[i])
	}
}
