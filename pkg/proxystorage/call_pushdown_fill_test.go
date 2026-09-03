package proxystorage

import (
	"context"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// fixedResultAPI is a stubAPI whose queries return a canned SeriesSet. The
// Call branch of NodeReplacer post-processes exactly that set, which is what
// these tests are about.
type fixedResultAPI struct {
	stubAPI
	result func() storage.SeriesSet
}

func (a *fixedResultAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return a.result()
}

func (a *fixedResultAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return a.result()
}

// callBranchNode runs NodeReplacer over a *parser.Call query whose downstream
// response is the given SeriesSet.
func callBranchNode(t *testing.T, result func() storage.SeriesSet, start, end time.Time, interval time.Duration) (parser.Node, error) {
	t.Helper()
	ps := &ProxyStorage{}
	ps.state.Store(&proxyStorageState{client: &fixedResultAPI{result: result}})

	expr, err := parser.ParseExpr(`present_over_time(foo[1m])`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	stmt := &parser.EvalStmt{Expr: expr, Start: start, End: end, Interval: interval}
	return ps.NodeReplacer(context.TODO(), stmt, expr, nil)
}

// TestCallPushdownHistogramAbandonsPushdown covers the histogram detection that
// now rides along inside the StaleNaN fill (s.Interval > 0): a downstream
// response carrying a native histogram sample must still be recognized as lossy
// and abandon pushdown (nil node, nil error) so the engine evaluates locally
// through remote_read.
func TestCallPushdownHistogramAbandonsPushdown(t *testing.T) {
	fh := (&histogram.Histogram{
		Count:           4,
		Sum:             3.5,
		Schema:          0,
		PositiveSpans:   []histogram.Span{{Offset: 0, Length: 2}},
		PositiveBuckets: []int64{2, 0},
	}).ToFloat(nil)

	result := func() storage.SeriesSet {
		return promapi.NewSeriesSet([]storage.Series{
			promapi.NewSeries(labels.FromStrings("__name__", "foo", "instance", "a"),
				[]chunks.Sample{promapi.FloatSample(0, 1)}),
			// The histogram lands on the second series — detection has to
			// survive the whole walk, not just the first series.
			promapi.NewSeries(labels.FromStrings("__name__", "foo", "instance", "b"),
				[]chunks.Sample{promapi.HistogramSample(60000, fh)}),
		}, nil, nil)
	}

	node, err := callBranchNode(t, result, time.Unix(0, 0), time.Unix(300, 0), time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Fatalf("histogram result must abandon pushdown, got node %s", node.String())
	}
}

// TestCallPushdownHistogramAbandonsPushdownInstant is the s.Interval <= 0 leg:
// no fill runs, so plain containsLossyHistogram still has to do the detection.
func TestCallPushdownHistogramAbandonsPushdownInstant(t *testing.T) {
	fh := (&histogram.Histogram{
		Count:           2,
		Sum:             1.5,
		PositiveSpans:   []histogram.Span{{Offset: 0, Length: 1}},
		PositiveBuckets: []int64{2},
	}).ToFloat(nil)

	result := func() storage.SeriesSet {
		return promapi.NewSeriesSet([]storage.Series{
			promapi.NewSeries(labels.FromStrings("__name__", "foo"),
				[]chunks.Sample{promapi.HistogramSample(10000000, fh)}),
		}, nil, nil)
	}

	now := time.Unix(10000, 0)
	node, err := callBranchNode(t, result, now, now, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node != nil {
		t.Fatalf("histogram result must abandon pushdown, got node %s", node.String())
	}
}

// TestCallPushdownFloatResultFilled is the happy path: a pure-float downstream
// response is not lossy, so the (single-pass) fill's output is what reaches
// UnexpandedSeriesSet — StaleNaN at every step the downstream skipped, the real
// samples untouched, and the set fresh rather than drained.
func TestCallPushdownFloatResultFilled(t *testing.T) {
	result := func() storage.SeriesSet {
		return promapi.NewSeriesSet([]storage.Series{
			promapi.NewSeries(labels.FromStrings("__name__", "foo"),
				[]chunks.Sample{promapi.FloatSample(0, 1), promapi.FloatSample(120000, 2)}),
		}, nil, nil)
	}

	node, err := callBranchNode(t, result, time.Unix(0, 0), time.Unix(300, 0), time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	vs, ok := node.(*parser.VectorSelector)
	if !ok {
		t.Fatalf("expected a *parser.VectorSelector, got %T", node)
	}
	if vs.UnexpandedSeriesSet == nil {
		t.Fatal("UnexpandedSeriesSet not set")
	}

	// The value handed to the engine must be a fresh, unconsumed set whose
	// samples were copied out of the (now spent) downstream cursor.
	got := drainSeriesSet(t, vs.UnexpandedSeriesSet)
	pts := got[`{__name__="foo"}`]
	if len(pts) != 6 {
		t.Fatalf("expected 6 points (one per step), got %d: %+v", len(pts), pts)
	}
	want := []struct {
		t     int64
		v     float64
		stale bool
	}{
		{0, 1, false},
		{60000, 0, true},
		{120000, 2, false},
		{180000, 0, true},
		{240000, 0, true},
		{300000, 0, true},
	}
	for i, w := range want {
		p := pts[i]
		if p.hist {
			t.Fatalf("point %d unexpectedly a histogram", i)
		}
		if p.t != w.t {
			t.Fatalf("point %d ts = %d, want %d", i, p.t, w.t)
		}
		if w.stale {
			if !p.stale {
				t.Fatalf("point %d (ts %d) should be a StaleNaN marker, got %v", i, p.t, p.v)
			}
			continue
		}
		if p.stale || p.v != w.v {
			t.Fatalf("point %d (ts %d) = %v (stale=%v), want %v", i, p.t, p.v, p.stale, w.v)
		}
	}
}

// TestFillStaleNaNGapsDetectHistogramReiterable pins the guarantee
// containsLossyHistogram documented and the combined pass inherits: the source
// cursor is drained exactly once, and what comes back is a fresh, unconsumed
// set of copied series whose sample iterators can be re-created at will.
func TestFillStaleNaNGapsDetectHistogramReiterable(t *testing.T) {
	src := promapi.NewSeriesSet([]storage.Series{
		promapi.NewSeries(labels.FromStrings("__name__", "foo"),
			[]chunks.Sample{promapi.FloatSample(0, 1)}),
	}, nil, nil)

	out, hasHist := fillStaleNaNGapsDetectHistogram(src, 0, 120000, 60000)
	if hasHist {
		t.Fatal("pure-float input reported as histogram-bearing")
	}
	if out == src {
		t.Fatal("expected a fresh materialized set, not the source cursor")
	}

	// The returned set is fresh (not the drained cursor) and every series in
	// it holds copied samples that can be iterated over and over.
	var series []storage.Series
	for out.Next() {
		series = append(series, out.At())
	}
	if err := out.Err(); err != nil {
		t.Fatalf("series set error: %v", err)
	}
	if len(series) != 1 {
		t.Fatalf("got %d series, want 1", len(series))
	}
	for pass := 0; pass < 3; pass++ {
		var n int
		it := series[0].Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			n++
		}
		if err := it.Err(); err != nil {
			t.Fatalf("pass %d: iterator error: %v", pass, err)
		}
		if n != 3 {
			t.Fatalf("pass %d: got %d points, want 3", pass, n)
		}
	}

	// The source cursor is spent — proof the fill consumed it exactly once.
	if src.Next() {
		t.Fatal("source cursor was not drained")
	}
}

// benchSeriesSet builds nSeries series of nSteps float samples on a step grid,
// with a fraction of the steps missing so the fill has real work to do. The
// series are built once and re-wrapped per iteration (promapi series are
// re-iterable), so the benchmark measures the post-processing, not the fixture.
func benchSeriesSet(nSeries, nSteps int, interval int64) []storage.Series {
	series := make([]storage.Series, 0, nSeries)
	for i := 0; i < nSeries; i++ {
		samples := make([]chunks.Sample, 0, nSteps)
		for j := 0; j < nSteps; j++ {
			if j%10 == 7 { // ~10% of the steps have no downstream value
				continue
			}
			samples = append(samples, promapi.FloatSample(int64(j)*interval, float64(j)))
		}
		series = append(series, promapi.NewSeries(
			labels.FromStrings("__name__", "foo", "instance", string(rune('a'+i%26)), "shard", string(rune('0'+i/26))),
			samples))
	}
	return series
}

// BenchmarkCallPushdownPostProcess compares the two arrangements of the
// *parser.Call branch's post-processing over a realistic result set
// (100 series x 1,000 steps):
//
//	two_pass:    containsLossyHistogram (copy #1) + fillStaleNaNGaps (copy #2)
//	single_pass: fillStaleNaNGapsDetectHistogram (one copy, both answers)
func BenchmarkCallPushdownPostProcess(b *testing.B) {
	const (
		nSeries  = 100
		nSteps   = 1000
		interval = int64(15000)
	)
	series := benchSeriesSet(nSeries, nSteps, interval)
	endTs := int64(nSteps-1) * interval

	b.Run("two_pass", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			in := promapi.NewSeriesSet(series, nil, nil)
			out, lossy := containsLossyHistogram(in)
			if lossy {
				b.Fatal("unexpected histogram")
			}
			consumeSeriesSet(b, fillStaleNaNGaps(out, 0, endTs, interval))
		}
	})

	b.Run("single_pass", func(b *testing.B) {
		b.ReportAllocs()
		for i := 0; i < b.N; i++ {
			in := promapi.NewSeriesSet(series, nil, nil)
			out, lossy := fillStaleNaNGapsDetectHistogram(in, 0, endTs, interval)
			if lossy {
				b.Fatal("unexpected histogram")
			}
			consumeSeriesSet(b, out)
		}
	})
}

// consumeSeriesSet reads the whole set so the benchmark accounts for the work
// the engine would do downstream of the fill.
func consumeSeriesSet(b *testing.B, ss storage.SeriesSet) {
	b.Helper()
	var n int
	for ss.Next() {
		it := ss.At().Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			if vt == chunkenc.ValFloat {
				if _, v := it.At(); value.IsStaleNaN(v) {
					n++
				}
			}
		}
	}
	if err := ss.Err(); err != nil {
		b.Fatal(err)
	}
	if n == 0 {
		b.Fatal("no stale markers emitted")
	}
}
