package proxystorage

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// readPoint is a decoded sample: timestamp, value (with StaleNaN flagged
// separately since NaN != NaN), and whether the point was a histogram.
type readPoint struct {
	t     int64
	v     float64
	stale bool
	hist  bool
}

// drainSeriesSet flattens a SeriesSet into per-series point slices keyed by the
// series' label string, so assertions don't depend on iterator plumbing.
func drainSeriesSet(t *testing.T, ss storage.SeriesSet) map[string][]readPoint {
	t.Helper()
	out := map[string][]readPoint{}
	for ss.Next() {
		s := ss.At()
		var pts []readPoint
		it := s.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				ts, v := it.At()
				pts = append(pts, readPoint{t: ts, v: v, stale: value.IsStaleNaN(v)})
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				ts, fh := it.AtFloatHistogram(nil)
				pts = append(pts, readPoint{t: ts, v: fh.Sum, hist: true})
			}
		}
		if err := it.Err(); err != nil {
			t.Fatalf("iterator error: %v", err)
		}
		out[s.Labels().String()] = pts
	}
	if err := ss.Err(); err != nil {
		t.Fatalf("series set error: %v", err)
	}
	return out
}

func oneSeriesSet(samples []chunks.Sample) storage.SeriesSet {
	s := promapi.NewSeries(labels.FromStrings(model.MetricNameLabel, "foo"), samples)
	return promapi.NewSeriesSet([]storage.Series{s}, nil, nil)
}

func assertSorted(t *testing.T, pts []readPoint) {
	t.Helper()
	for i := 1; i < len(pts); i++ {
		if pts[i-1].t > pts[i].t {
			t.Fatalf("samples out of timestamp order at index %d: %d > %d", i, pts[i-1].t, pts[i].t)
		}
	}
}

// TestFillStaleNaNGapsSparseWideRange is the regression test for the removed
// `expected <= len(present)+10_000` bound: an 11k-step range holding a single
// sample used to skip the fill entirely, letting the engine's lookback bleed
// that sample forward across the whole range.
func TestFillStaleNaNGapsSparseWideRange(t *testing.T) {
	const (
		start    = int64(0)
		interval = int64(1000)
		steps    = 11000
	)
	end := start + interval*(steps-1)
	hitTs := start + interval*5000

	in := oneSeriesSet([]chunks.Sample{promapi.FloatSample(hitTs, 1)})
	got := drainSeriesSet(t, fillStaleNaNGaps(in, start, end, interval))

	pts := got[`{__name__="foo"}`]
	if len(pts) != steps {
		t.Fatalf("got %d samples, want %d (fill was skipped)", len(pts), steps)
	}
	assertSorted(t, pts)
	for i, p := range pts {
		wantTs := start + interval*int64(i)
		if p.t != wantTs {
			t.Fatalf("sample %d has ts %d, want %d", i, p.t, wantTs)
		}
		if p.t == hitTs {
			if p.stale || p.v != 1 {
				t.Fatalf("real sample at %d was overwritten: %+v", hitTs, p)
			}
			continue
		}
		if !p.stale {
			t.Fatalf("step %d (ts %d) is not a StaleNaN marker: %+v", i, p.t, p)
		}
	}
}

// TestFillStaleNaNGapsDense verifies a series covering every step is returned
// unchanged — no markers inserted, values preserved.
func TestFillStaleNaNGapsDense(t *testing.T) {
	const (
		start    = int64(1000)
		interval = int64(500)
		steps    = 20
	)
	end := start + interval*(steps-1)

	var src []chunks.Sample
	for i := 0; i < steps; i++ {
		src = append(src, promapi.FloatSample(start+interval*int64(i), float64(i)))
	}

	got := drainSeriesSet(t, fillStaleNaNGaps(oneSeriesSet(src), start, end, interval))
	pts := got[`{__name__="foo"}`]
	if len(pts) != steps {
		t.Fatalf("got %d samples, want %d", len(pts), steps)
	}
	assertSorted(t, pts)
	for i, p := range pts {
		if p.stale {
			t.Fatalf("unexpected StaleNaN at index %d (ts %d)", i, p.t)
		}
		if p.t != start+interval*int64(i) || p.v != float64(i) {
			t.Fatalf("sample %d = %+v, want ts %d value %d", i, p, start+interval*int64(i), i)
		}
	}
}

// TestFillStaleNaNGapsOffGridSamplesPreserved covers source samples that don't
// land on the step grid: they must survive the merge and stay in timestamp
// order relative to the inserted markers.
func TestFillStaleNaNGapsOffGridSamplesPreserved(t *testing.T) {
	const (
		start    = int64(0)
		end      = int64(300)
		interval = int64(100)
	)
	// Grid is 0,100,200,300. Source has an on-grid sample at 100 plus off-grid
	// samples before the grid, between steps, and past the end.
	src := []chunks.Sample{
		promapi.FloatSample(-50, 10),
		promapi.FloatSample(100, 11),
		promapi.FloatSample(150, 12),
		promapi.FloatSample(275, 13),
		promapi.FloatSample(400, 14),
	}

	got := drainSeriesSet(t, fillStaleNaNGaps(oneSeriesSet(src), start, end, interval))
	pts := got[`{__name__="foo"}`]
	assertSorted(t, pts)

	wants := []readPoint{
		{t: -50, v: 10},
		{t: 0, stale: true},
		{t: 100, v: 11},
		{t: 150, v: 12},
		{t: 200, stale: true},
		{t: 275, v: 13},
		{t: 300, stale: true},
		{t: 400, v: 14},
	}
	if len(pts) != len(wants) {
		t.Fatalf("got %d samples %+v, want %d", len(pts), pts, len(wants))
	}
	for i, w := range wants {
		p := pts[i]
		if p.t != w.t || p.stale != w.stale {
			t.Fatalf("sample %d = %+v, want ts %d stale %v", i, p, w.t, w.stale)
		}
		if !w.stale && p.v != w.v {
			t.Fatalf("sample %d value = %v, want %v", i, p.v, w.v)
		}
	}
}

// TestFillStaleNaNGapsHistograms verifies histogram samples survive the merge
// (neither dropped nor flattened to floats) while the gaps around them still
// get markers.
func TestFillStaleNaNGapsHistograms(t *testing.T) {
	const (
		start    = int64(0)
		end      = int64(300)
		interval = int64(100)
	)
	mkHist := func(sum float64) *histogram.FloatHistogram {
		return &histogram.FloatHistogram{
			Count:           3,
			Sum:             sum,
			PositiveSpans:   []histogram.Span{{Offset: 0, Length: 1}},
			PositiveBuckets: []float64{3},
		}
	}
	src := []chunks.Sample{
		promapi.HistogramSample(100, mkHist(1.5)),
		promapi.HistogramSample(300, mkHist(2.5)),
	}

	got := drainSeriesSet(t, fillStaleNaNGaps(oneSeriesSet(src), start, end, interval))
	pts := got[`{__name__="foo"}`]
	if len(pts) != 4 {
		t.Fatalf("got %d samples, want 4", len(pts))
	}
	assertSorted(t, pts)
	for i, p := range pts {
		switch p.t {
		case 100, 300:
			if !p.hist {
				t.Fatalf("sample %d (ts %d) lost its histogram: %+v", i, p.t, p)
			}
		default:
			if p.hist || !p.stale {
				t.Fatalf("sample %d (ts %d) should be a StaleNaN marker: %+v", i, p.t, p)
			}
		}
	}
	if pts[1].v != 1.5 || pts[3].v != 2.5 {
		t.Fatalf("histogram sums mangled: %v, %v", pts[1].v, pts[3].v)
	}
}

// TestFillStaleNaNGapsNoInterval covers the instant-query path: the input set
// is returned untouched, not materialized.
func TestFillStaleNaNGapsNoInterval(t *testing.T) {
	in := oneSeriesSet([]chunks.Sample{promapi.FloatSample(100, 1)})
	for _, interval := range []int64{0, -1000} {
		if out := fillStaleNaNGaps(in, 0, 300, interval); out != in {
			t.Fatalf("interval %d: got a new series set, want the input unchanged", interval)
		}
	}
	pts := drainSeriesSet(t, in)[`{__name__="foo"}`]
	if len(pts) != 1 || pts[0].t != 100 || pts[0].v != 1 {
		t.Fatalf("input series set was modified: %+v", pts)
	}
}
