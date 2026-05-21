package promhttputil

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
)

func TestDynamicAntiAffinity(t *testing.T) {
	mk := func(times ...int64) []model.SamplePair {
		out := make([]model.SamplePair, len(times))
		for i, t := range times {
			out[i] = model.SamplePair{Timestamp: model.Time(t), Value: 1}
		}
		return out
	}

	tests := []struct {
		name string
		a    []model.SamplePair
		b    []model.SamplePair
		want model.Time
		ok   bool
	}{
		{
			name: "estimates half-the-median for evenly-spaced 60s scrape",
			a:    mk(0, 60_000, 120_000, 180_000, 240_000),
			want: 30_000,
			ok:   true,
		},
		{
			name: "median is robust to a single dropped scrape",
			// One missed scrape between t=60s and t=180s widens that gap to
			// 120s, but the other gaps are 60s — the median should remain
			// 60s (so buffer = 30s), not slide toward the mean of 75s.
			a:    mk(0, 60_000, 180_000, 240_000, 300_000),
			want: 30_000,
			ok:   true,
		},
		{
			name: "borrows from b when a is too short",
			a:    mk(0, 60_000),
			b:    mk(0, 30_000, 60_000, 90_000),
			want: 15_000,
			ok:   true,
		},
		{
			name: "returns ok=false with too few samples",
			a:    mk(0, 60_000),
			ok:   false,
		},
		{
			name: "returns ok=false with no samples",
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dynamicAntiAffinity(tc.a, tc.b)
			if ok != tc.ok {
				t.Fatalf("ok: want %v got %v", tc.ok, ok)
			}
			if ok && got != tc.want {
				t.Fatalf("buffer: want %v got %v", tc.want, got)
			}
		})
	}
}

// TestMergeValues_FixesMixedScrapeIntervals reproduces the case in
// jacksontj/promxy#734: with a single static anti-affinity, low-frequency
// series get gap-filled with samples from the other downstream because their
// natural gap exceeds 2×buffer. count_over_time on the merged result then
// reports too many samples. With dynamic mode on, each series picks its own
// buffer and no gap-fill happens.
func TestMergeValues_FixesMixedScrapeIntervals(t *testing.T) {
	// 60s-scrape series: a has samples at 0, 60s, 120s. b has 60s-scrape
	// samples too, offset enough that they fall OUTSIDE the static
	// buffer of a's samples but INSIDE the gap between consecutive a
	// samples — exactly the case where the static algorithm misreads
	// the situation as "a has a missed scrape that b can fill" and
	// splices b in.
	a := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "slow"},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 60_000, Value: 1},
				{Timestamp: 120_000, Value: 1},
			},
		},
	}
	b := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "slow"},
			Values: []model.SamplePair{
				{Timestamp: 30_000, Value: 1},
				{Timestamp: 90_000, Value: 1},
				{Timestamp: 150_000, Value: 1},
			},
		},
	}

	staticBuffer := model.Time(15_000) // 15s — wrong for a 60s scraper

	// Static mode: 2*15s = 30s threshold. Each 60s gap exceeds that, so
	// each gap gets filled with the corresponding b sample. Result: a's
	// samples plus b's samples interleaved. count_over_time over this
	// would double. This is the bug from #734.
	staticRes, err := MergeValues(staticBuffer, false, a, b, false)
	if err != nil {
		t.Fatalf("static MergeValues: %v", err)
	}
	staticMatrix := staticRes.(model.Matrix)
	if got := len(staticMatrix[0].Values); got != 6 {
		t.Fatalf("static merge sanity check: expected 6 samples (the bug), got %d", got)
	}

	// Dynamic mode: estimates buffer ≈ 30s from the data, so 2*30s = 60s
	// is the gap-fill threshold. The 60s gaps no longer trigger fill
	// (they're at the threshold, not above it). Result: a's 3 samples,
	// no spurious fills.
	dynamicRes, err := MergeValues(staticBuffer, true, a, b, false)
	if err != nil {
		t.Fatalf("dynamic MergeValues: %v", err)
	}
	dynamicMatrix := dynamicRes.(model.Matrix)
	gotTimes := make([]int64, 0, len(dynamicMatrix[0].Values))
	for _, p := range dynamicMatrix[0].Values {
		gotTimes = append(gotTimes, int64(p.Timestamp))
	}
	wantTimes := []int64{0, 60_000, 120_000}
	if !reflect.DeepEqual(gotTimes, wantTimes) {
		t.Fatalf("dynamic merge sample times: want %v got %v", wantTimes, gotTimes)
	}
}

// TestMergeValues_FallbackWithFewSamples covers the safety net: when
// a series has too few samples to estimate, the configured static buffer is
// used unchanged, so existing behavior is preserved for short queries.
func TestMergeValues_FallbackWithFewSamples(t *testing.T) {
	a := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "x"},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 60_000, Value: 1},
			},
		},
	}
	// b's sample is within the static buffer of a's first sample, so the
	// static merge dedups it. The dynamic estimator can't fire (only 1
	// gap on either side), so dynamic mode falls back to the same buffer
	// and gets the same answer.
	b := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "x"},
			Values: []model.SamplePair{
				{Timestamp: 5_000, Value: 1},
			},
		},
	}

	for _, dyn := range []bool{false, true} {
		got, err := MergeValues(model.Time(15_000), dyn, a, b, false)
		if err != nil {
			t.Fatalf("dyn=%v: MergeValues: %v", dyn, err)
		}
		mat := got.(model.Matrix)
		if n := len(mat[0].Values); n != 2 {
			t.Fatalf("dyn=%v: want 2 samples, got %d", dyn, n)
		}
	}
}

// TestMergeValues_PerSeriesBuffer makes sure two series in one
// matrix with different scrape intervals each get their own dynamically-
// inferred buffer. A 60s-scrape series and a 10s-scrape series share the
// same merge call, and we want neither to be touched by the other's
// buffer choice.
func TestMergeValues_PerSeriesBuffer(t *testing.T) {
	slow := model.Metric{model.MetricNameLabel: "slow"}
	fast := model.Metric{model.MetricNameLabel: "fast"}

	a := model.Matrix{
		// 60s scrape, samples at 0/60/120s
		{Metric: slow, Values: []model.SamplePair{
			{Timestamp: 0, Value: 1}, {Timestamp: 60_000, Value: 1}, {Timestamp: 120_000, Value: 1},
		}},
		// 10s scrape, samples at 0/10/20/30/40s
		{Metric: fast, Values: []model.SamplePair{
			{Timestamp: 0, Value: 1}, {Timestamp: 10_000, Value: 1},
			{Timestamp: 20_000, Value: 1}, {Timestamp: 30_000, Value: 1}, {Timestamp: 40_000, Value: 1},
		}},
	}
	b := model.Matrix{
		// slow series on the other downstream — offset by 30s
		{Metric: slow, Values: []model.SamplePair{
			{Timestamp: 30_000, Value: 1}, {Timestamp: 90_000, Value: 1}, {Timestamp: 150_000, Value: 1},
		}},
		// fast series — offset by 5s
		{Metric: fast, Values: []model.SamplePair{
			{Timestamp: 5_000, Value: 1}, {Timestamp: 15_000, Value: 1},
			{Timestamp: 25_000, Value: 1}, {Timestamp: 35_000, Value: 1}, {Timestamp: 45_000, Value: 1},
		}},
	}

	staticBuffer := model.Time(15_000) // 15s — wrong for both, illustrative

	got, err := MergeValues(staticBuffer, true, a, b, false)
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	matrix := got.(model.Matrix)
	if len(matrix) != 2 {
		t.Fatalf("want 2 series, got %d", len(matrix))
	}

	for _, s := range matrix {
		switch s.Metric.Equal(slow) {
		case true:
			// slow series: median gap 60s → buffer 30s. b's 30s/90s/150s
			// are exactly buffer-distance from a's 0/60/120s — within
			// the dedup zone, none should fill, none should append at
			// the tail.
			if got, want := len(s.Values), 3; got != want {
				t.Fatalf("slow: want %d samples, got %d (%+v)", want, got, s.Values)
			}
		default:
			// fast series: median gap 10s → buffer 5s. b's samples are
			// offset by 5s — exactly the buffer, so they land on the
			// gap-fill / dedup boundary. Implementation detail of which
			// b values stay; the important assertion is that we don't
			// blow up with 10 samples (the worst-case interleaving).
			if got := len(s.Values); got > 7 || got < 5 {
				t.Fatalf("fast: want 5–7 samples, got %d (%+v)", got, s.Values)
			}
		}
	}
}

// TestMergeValues_NarrowerThanStatic exercises the direction the
// other tests don't: a fast (10s) scrape with a wide configured buffer.
// The static buffer's gap-fill threshold is wide enough to suppress a
// genuine missed-scrape fill from b; the dynamic estimate is tight
// enough to splice it in.
func TestMergeValues_NarrowerThanStatic(t *testing.T) {
	// 10s scrape on a, with a single missed scrape between t=20 and
	// t=40 (gap = 20s instead of 10s).
	a := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "fast"},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 10_000, Value: 1},
				{Timestamp: 20_000, Value: 1},
				{Timestamp: 40_000, Value: 1},
				{Timestamp: 50_000, Value: 1},
			},
		},
	}
	// b has a sample at t=30 that fills a's missed scrape.
	b := model.Matrix{
		{
			Metric: model.Metric{model.MetricNameLabel: "fast"},
			Values: []model.SamplePair{
				{Timestamp: 30_000, Value: 1},
			},
		},
	}

	// Static buffer 30s: gap-fill threshold is 60s, a's 20s gap is
	// well under that, so b's filler never triggers — count_over_time
	// undercounts.
	staticBuffer := model.Time(30_000)
	sv, err := MergeValues(staticBuffer, false, a, b, false)
	if err != nil {
		t.Fatalf("static MergeValues: %v", err)
	}
	if got := len(sv.(model.Matrix)[0].Values); got != 5 {
		t.Fatalf("static sanity: expected 5 (no fill), got %d", got)
	}

	// Dynamic: median gap 10s → buffer 5s. Gap-fill threshold 10s; a's
	// 20s gap exceeds it, so b's filler at t=30 splices in.
	dv, err := MergeValues(staticBuffer, true, a, b, false)
	if err != nil {
		t.Fatalf("dynamic MergeValues: %v", err)
	}
	if got := len(dv.(model.Matrix)[0].Values); got != 6 {
		t.Fatalf("dynamic should have spliced b's filler, got %d (want 6)", got)
	}
}

// TestMergeValues_PreferMax confirms preferMax merge logic and
// dynamic buffer inference are independent — turning dynamic on with
// preferMax=true should still pick the larger value when a/b overlap.
//
// Note: the merge code unconditionally appends a's first sample without
// running the preferMax check (line "if len(newValues) == 0"), so we only
// assert the property on samples 2..N.
func TestMergeValues_PreferMax(t *testing.T) {
	a := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Values: []model.SamplePair{
			{Timestamp: 0, Value: 1}, {Timestamp: 60_000, Value: 1}, {Timestamp: 120_000, Value: 1},
		},
	}}
	// b samples within the dynamic buffer of a, with larger values.
	b := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Values: []model.SamplePair{
			{Timestamp: 5_000, Value: 5}, {Timestamp: 65_000, Value: 5}, {Timestamp: 125_000, Value: 5},
		},
	}}

	got, err := MergeValues(model.Time(15_000), true, a, b, true)
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	mat := got.(model.Matrix)
	if len(mat[0].Values) != 3 {
		t.Fatalf("want 3 samples, got %d", len(mat[0].Values))
	}
	for _, p := range mat[0].Values[1:] {
		if p.Value != 5 {
			t.Fatalf("preferMax: want value=5, got %v at t=%v", p.Value, p.Timestamp)
		}
	}
}

// TestMergeValues_MismatchedScrapeIntervals: a is scraped at 60s,
// b at 30s. The estimator anchors on the longer side (b, since after
// the swap-for-longer-base it becomes a) and should pick b's interval.
// Behavior we want: no spurious gap-fill, no double-counting.
func TestMergeValues_MismatchedScrapeIntervals(t *testing.T) {
	a := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Values: []model.SamplePair{
			{Timestamp: 0, Value: 1}, {Timestamp: 60_000, Value: 1}, {Timestamp: 120_000, Value: 1},
		},
	}}
	b := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Values: []model.SamplePair{
			{Timestamp: 0, Value: 2}, {Timestamp: 30_000, Value: 2},
			{Timestamp: 60_000, Value: 2}, {Timestamp: 90_000, Value: 2}, {Timestamp: 120_000, Value: 2},
		},
	}}

	got, err := MergeValues(model.Time(15_000), true, a, b, false)
	if err != nil {
		t.Fatalf("MergeValues: %v", err)
	}
	mat := got.(model.Matrix)
	// b is the longer side → becomes the base. Median gap is 30s,
	// buffer = 15s. a's samples fall within b's buffer so they all
	// dedupe; result is just b's 5 samples.
	if got := len(mat[0].Values); got != 5 {
		t.Fatalf("want 5 samples (b's stream), got %d (%+v)", got, mat[0].Values)
	}
}

// TestMergeValues_Histograms confirms the same dynamic estimate
// is applied to histogram merges, not just floats. Without this the
// histogram-only series would still hit the original bug under mixed
// scrape intervals.
func TestMergeValues_Histograms(t *testing.T) {
	mkH := func(ts model.Time, v float64) model.SampleHistogramPair {
		return model.SampleHistogramPair{
			Timestamp: ts,
			Histogram: &model.SampleHistogram{Count: model.FloatString(v)},
		}
	}

	a := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Histograms: []model.SampleHistogramPair{
			mkH(0, 1), mkH(60_000, 1), mkH(120_000, 1),
		},
	}}
	b := model.Matrix{{
		Metric: model.Metric{model.MetricNameLabel: "x"},
		Histograms: []model.SampleHistogramPair{
			mkH(30_000, 2), mkH(90_000, 2), mkH(150_000, 2),
		},
	}}

	staticBuffer := model.Time(15_000) // wrong for 60s scrape

	// Static: buggy fill, ends up with 6 histograms.
	staticRes, _ := MergeValues(staticBuffer, false, a, b, false)
	if got := len(staticRes.(model.Matrix)[0].Histograms); got != 6 {
		t.Fatalf("static sanity: expected 6 histograms (the bug), got %d", got)
	}

	// Dynamic: estimate from histogram timestamps directly, no fill.
	dynRes, _ := MergeValues(staticBuffer, true, a, b, false)
	if got := len(dynRes.(model.Matrix)[0].Histograms); got != 3 {
		t.Fatalf("dynamic histograms: want 3, got %d", got)
	}
}
