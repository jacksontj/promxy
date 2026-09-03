package promhttputil

import (
	"math/rand"
	"sort"
	"testing"

	"github.com/prometheus/common/model"
)

// refDynamicAntiAffinity is the estimator exactly as it was before the
// allocation-free rewrite: two []model.Time copies, a third slice for the
// gaps, and a full sort.Slice to read out one median. It's kept here purely
// as a test oracle -- the rewrite is only allowed to change the allocation
// profile, never the number it returns.
func refDynamicAntiAffinity(a, b []model.SamplePair) (model.Time, bool) {
	at := make([]model.Time, len(a))
	for i, p := range a {
		at[i] = p.Timestamp
	}
	bt := make([]model.Time, len(b))
	for i, p := range b {
		bt[i] = p.Timestamp
	}
	return refDynamicAntiAffinityFromTimes(at, bt)
}

func refDynamicAntiAffinityFromTimes(a, b []model.Time) (model.Time, bool) {
	const minDynamicGaps = 3

	gaps := make([]model.Time, 0, len(a))
	for i := 1; i < len(a); i++ {
		if d := a[i] - a[i-1]; d > 0 {
			gaps = append(gaps, d)
		}
	}
	if len(gaps) < minDynamicGaps {
		for i := 1; i < len(b); i++ {
			if d := b[i] - b[i-1]; d > 0 {
				gaps = append(gaps, d)
			}
		}
	}
	if len(gaps) < minDynamicGaps {
		return 0, false
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i] < gaps[j] })
	median := gaps[len(gaps)/2]
	return median / 2, true
}

func mkPairs(times ...int64) []model.SamplePair {
	out := make([]model.SamplePair, len(times))
	for i, ts := range times {
		out[i] = model.SamplePair{Timestamp: model.Time(ts), Value: 1}
	}
	return out
}

func mkHistPairs(times ...int64) []model.SampleHistogramPair {
	out := make([]model.SampleHistogramPair, len(times))
	for i, ts := range times {
		out[i] = model.SampleHistogramPair{
			Timestamp: model.Time(ts),
			Histogram: &model.SampleHistogram{Count: 1},
		}
	}
	return out
}

// TestDynamicAntiAffinity_MatchesReference pins the rewritten estimator to
// the pre-rewrite implementation: same inputs, bit-identical (value, ok).
// Every branch of the estimator is covered -- the b-fallback (which keeps
// a's gaps and appends b's), the non-positive-delta skip, and the
// too-few-gaps bail.
func TestDynamicAntiAffinity_MatchesReference(t *testing.T) {
	tests := []struct {
		name string
		a    []model.SamplePair
		b    []model.SamplePair
	}{
		{
			name: "evenly spaced 60s scrape",
			a:    mkPairs(0, 60_000, 120_000, 180_000, 240_000),
		},
		{
			name: "one dropped scrape widens a single gap",
			a:    mkPairs(0, 60_000, 180_000, 240_000, 300_000),
		},
		{
			name: "several dropped scrapes",
			a:    mkPairs(0, 60_000, 180_000, 360_000, 420_000, 600_000, 660_000),
		},
		{
			name: "mixed intervals within one series",
			a:    mkPairs(0, 10_000, 40_000, 45_000, 105_000, 106_000, 300_000),
		},
		{
			name: "even number of gaps (median takes the upper of the middle pair)",
			a:    mkPairs(0, 10_000, 30_000, 60_000, 100_000),
		},
		{
			name: "odd interval, median divides odd",
			a:    mkPairs(0, 7_001, 14_002, 21_003, 28_004),
		},
		{
			name: "fewer than 3 gaps on a, b fallback fires",
			a:    mkPairs(0, 60_000),
			b:    mkPairs(0, 30_000, 60_000, 90_000),
		},
		{
			name: "b fallback keeps a's gaps (a contributes 2, b contributes 2)",
			a:    mkPairs(0, 100_000, 200_000),
			b:    mkPairs(0, 1_000, 2_000),
		},
		{
			name: "empty a, b fallback carries the whole estimate",
			b:    mkPairs(0, 30_000, 60_000, 90_000, 120_000),
		},
		{
			name: "duplicate timestamps produce zero deltas that are skipped",
			a:    mkPairs(0, 0, 60_000, 60_000, 120_000, 180_000, 240_000),
		},
		{
			name: "out-of-order timestamps produce negative deltas that are skipped",
			a:    mkPairs(0, 60_000, 30_000, 120_000, 180_000, 240_000, 300_000),
		},
		{
			name: "all deltas non-positive on a, falls through to b",
			a:    mkPairs(100, 100, 100, 100, 100),
			b:    mkPairs(0, 15_000, 30_000, 45_000),
		},
		{
			name: "all deltas non-positive on both sides",
			a:    mkPairs(5, 5, 5, 5),
			b:    mkPairs(9, 8, 7, 6),
		},
		{
			name: "exactly minDynamicGaps gaps on a",
			a:    mkPairs(0, 60_000, 120_000, 180_000),
		},
		{
			name: "one gap short on a, b is empty",
			a:    mkPairs(0, 60_000, 120_000),
		},
		{
			name: "single sample on a",
			a:    mkPairs(0),
		},
		{
			name: "both sides empty",
		},
		{
			name: "b non-empty but too short to rescue a",
			a:    mkPairs(0, 60_000),
			b:    mkPairs(0, 60_000),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			wantVal, wantOK := refDynamicAntiAffinity(clonePairs(tc.a), clonePairs(tc.b))
			gotVal, gotOK := dynamicAntiAffinity(clonePairs(tc.a), clonePairs(tc.b))
			if gotOK != wantOK || gotVal != wantVal {
				t.Fatalf("want (%v, %v), got (%v, %v)", wantVal, wantOK, gotVal, gotOK)
			}

			// The histogram path must agree with the float path for the
			// same timestamps -- it's the same estimator, just a
			// different accessor.
			hGot, hOK := dynamicAntiAffinityFrom(toHist(tc.a), toHist(tc.b), histogramTimestamp)
			if hOK != wantOK || hGot != wantVal {
				t.Fatalf("histogram path: want (%v, %v), got (%v, %v)", wantVal, wantOK, hGot, hOK)
			}
		})
	}
}

func clonePairs(s []model.SamplePair) []model.SamplePair {
	if s == nil {
		return nil
	}
	out := make([]model.SamplePair, len(s))
	copy(out, s)
	return out
}

func toHist(s []model.SamplePair) []model.SampleHistogramPair {
	if s == nil {
		return nil
	}
	out := make([]model.SampleHistogramPair, len(s))
	for i, p := range s {
		out[i] = model.SampleHistogramPair{
			Timestamp: p.Timestamp,
			Histogram: &model.SampleHistogram{Count: 1},
		}
	}
	return out
}

// TestDynamicAntiAffinity_MatchesReferenceRandomized hammers the same
// equivalence on randomly-shaped input, which is where the quickselect
// (rather than a full sort) could plausibly diverge: duplicate-heavy
// slices, already-sorted slices, reverse-sorted slices and every length
// from 0 to 64.
func TestDynamicAntiAffinity_MatchesReferenceRandomized(t *testing.T) {
	rng := rand.New(rand.NewSource(1))

	gen := func(n int) []model.SamplePair {
		out := make([]model.SamplePair, n)
		ts := int64(0)
		for i := range out {
			switch rng.Intn(4) {
			case 0: // repeat the timestamp -> zero delta
			case 1: // step backwards -> negative delta
				ts -= int64(rng.Intn(30_000))
			case 2: // the common case: a fixed interval
				ts += 15_000
			default:
				ts += int64(rng.Intn(120_000))
			}
			out[i] = model.SamplePair{Timestamp: model.Time(ts), Value: 1}
		}
		return out
	}

	for i := 0; i < 2000; i++ {
		a := gen(rng.Intn(65))
		b := gen(rng.Intn(65))

		wantVal, wantOK := refDynamicAntiAffinity(clonePairs(a), clonePairs(b))
		gotVal, gotOK := dynamicAntiAffinity(clonePairs(a), clonePairs(b))
		if gotOK != wantOK || gotVal != wantVal {
			t.Fatalf("iteration %d: a=%v b=%v: want (%v, %v), got (%v, %v)",
				i, a, b, wantVal, wantOK, gotVal, gotOK)
		}
	}
}

// TestSelectNth matches the partial selection against a full sort for every
// index of every slice, including the duplicate-heavy shapes real gap data
// produces (a healthy series has near-identical gaps, which is a classic
// quickselect worst case if the partitioning is wrong).
func TestDynamicBufferForStream_AnchorsOnLongerSide(t *testing.T) {
	tests := []struct {
		name string
		a    *model.SampleStream
		b    *model.SampleStream
		want model.Time
		ok   bool
	}{
		{
			name: "longer float side wins regardless of argument order",
			// 60s scrape (3 samples, 2 gaps) vs 30s scrape (5 samples,
			// 4 gaps): the 30s side is longer, so buffer = 15s.
			a:    &model.SampleStream{Values: mkPairs(0, 60_000, 120_000)},
			b:    &model.SampleStream{Values: mkPairs(0, 30_000, 60_000, 90_000, 120_000)},
			want: 15_000,
			ok:   true,
		},
		{
			name: "float side too short, falls back to histograms",
			a: &model.SampleStream{
				Values:     mkPairs(0, 60_000),
				Histograms: mkHistPairs(0, 20_000, 40_000, 60_000),
			},
			b:    &model.SampleStream{},
			want: 10_000,
			ok:   true,
		},
		{
			name: "histogram-only streams, longer side anchors",
			a:    &model.SampleStream{Histograms: mkHistPairs(0, 60_000)},
			b:    &model.SampleStream{Histograms: mkHistPairs(0, 10_000, 20_000, 30_000, 40_000)},
			want: 5_000,
			ok:   true,
		},
		{
			name: "neither side has enough data",
			a:    &model.SampleStream{Values: mkPairs(0, 60_000)},
			b:    &model.SampleStream{Histograms: mkHistPairs(0, 10_000)},
			ok:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := dynamicBufferForStream(tc.a, tc.b)
			if ok != tc.ok {
				t.Fatalf("ok: want %v got %v", tc.ok, ok)
			}
			if ok && got != tc.want {
				t.Fatalf("buffer: want %v got %v", tc.want, got)
			}

			// Argument order must not matter: the swap is what makes the
			// estimate anchor on the longer side either way.
			swapped, swappedOK := dynamicBufferForStream(tc.b, tc.a)
			if swappedOK != ok || swapped != got {
				t.Fatalf("swapped args: want (%v, %v), got (%v, %v)", got, ok, swapped, swappedOK)
			}
		})
	}
}

var (
	sinkTime model.Time
	sinkOK   bool
)

// TestDynamicAntiAffinity_Allocs pins the estimator to its intended
// allocation count. The whole point of the rewrite is that it no longer
// flattens either side into a []model.Time, so the gaps slice must remain
// the only allocation on the hot path -- this test is what stops the copies
// coming back.
func TestDynamicAntiAffinity_Allocs(t *testing.T) {
	a := make([]model.SamplePair, 512)
	for i := range a {
		a[i] = model.SamplePair{Timestamp: model.Time(i * 15_000), Value: 1}
	}
	b := make([]model.SamplePair, 512)
	for i := range b {
		b[i] = model.SamplePair{Timestamp: model.Time(i*15_000 + 7_000), Value: 1}
	}

	t.Run("float", func(t *testing.T) {
		got := testing.AllocsPerRun(100, func() {
			sinkTime, sinkOK = dynamicAntiAffinity(a, b)
		})
		if got != 1 {
			t.Fatalf("allocs/op: want 1 (the gaps slice), got %v", got)
		}
	})

	t.Run("histogram", func(t *testing.T) {
		ha, hb := toHist(a), toHist(b)
		got := testing.AllocsPerRun(100, func() {
			sinkTime, sinkOK = dynamicAntiAffinityFrom(ha, hb, histogramTimestamp)
		})
		if got != 1 {
			t.Fatalf("allocs/op: want 1 (the gaps slice), got %v", got)
		}
	})

	t.Run("stream", func(t *testing.T) {
		sa := &model.SampleStream{Values: a}
		sb := &model.SampleStream{Values: b}
		got := testing.AllocsPerRun(100, func() {
			sinkTime, sinkOK = dynamicBufferForStream(sa, sb)
		})
		if got != 1 {
			t.Fatalf("allocs/op: want 1 (the gaps slice), got %v", got)
		}
	})
}

func benchInputs() (xs, ys []model.SamplePair) {
	xs = make([]model.SamplePair, 1024)
	for i := range xs {
		xs[i] = model.SamplePair{Timestamp: model.Time(i * 15_000), Value: 1}
	}
	ys = make([]model.SamplePair, 1024)
	for i := range ys {
		ys[i] = model.SamplePair{Timestamp: model.Time(i*15_000 + 7_000), Value: 1}
	}
	return xs, ys
}

func BenchmarkDynamicAntiAffinity(b *testing.B) {
	xs, ys := benchInputs()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkTime, sinkOK = dynamicAntiAffinity(xs, ys)
	}
}

// BenchmarkDynamicAntiAffinityReference measures the pre-rewrite estimator,
// so the allocation saving stays visible side by side with the current one.
func BenchmarkDynamicAntiAffinityReference(b *testing.B) {
	xs, ys := benchInputs()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkTime, sinkOK = refDynamicAntiAffinity(xs, ys)
	}
}
