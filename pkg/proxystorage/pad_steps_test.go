package proxystorage

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

type padSample struct {
	t      int64
	v      float64
	isHist bool
	sum    float64
}

func seriesSetOf(samples ...chunks.Sample) storage.SeriesSet {
	return promapi.NewSeriesSet(
		[]storage.Series{promapi.NewSeries(labels.EmptyLabels(), samples)},
		nil, nil,
	)
}

func readPadSeries(t *testing.T, ss storage.SeriesSet) []padSample {
	t.Helper()
	if !ss.Next() {
		if err := ss.Err(); err != nil {
			t.Fatalf("series set error: %v", err)
		}
		return nil
	}
	var out []padSample
	it := ss.At().Iterator(nil)
	for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
		switch vt {
		case chunkenc.ValFloat:
			ts, v := it.At()
			out = append(out, padSample{t: ts, v: v})
		case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
			ts, fh := it.AtFloatHistogram(nil)
			out = append(out, padSample{t: ts, isHist: true, sum: fh.Sum})
		}
	}
	if err := it.Err(); err != nil {
		t.Fatalf("iterator error: %v", err)
	}
	return out
}

func TestPadMissingStepsWithStaleNaN_FillsHistogramHoles(t *testing.T) {
	// Mirrors the functions.test mixed_metric case for round / clamp pushdown:
	// upstream returned floats at t=60s,120s,180s (the histogram-bearing
	// 0s/240s/300s steps were dropped by the function), and we want padding to
	// drop a StaleNaN at every missing step in [0s, 300s] so the engine's
	// PeekPrev fallback can't extend the last float across the gaps.
	in := seriesSetOf(
		promapi.FloatSample(60_000, 1),
		promapi.FloatSample(120_000, 2),
		promapi.FloatSample(180_000, 3),
	)
	out := readPadSeries(t, padMissingStepsWithStaleNaN(in, timestamp.Time(0), timestamp.Time(300_000), time.Minute))
	want := []struct {
		ts    int64
		stale bool
		val   float64
	}{
		{0, true, 0},
		{60_000, false, 1},
		{120_000, false, 2},
		{180_000, false, 3},
		{240_000, true, 0},
		{300_000, true, 0},
	}
	if len(out) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(out), out)
	}
	for i, w := range want {
		if out[i].t != w.ts {
			t.Errorf("idx %d: ts=%d want %d", i, out[i].t, w.ts)
		}
		if w.stale {
			if !value.IsStaleNaN(out[i].v) {
				t.Errorf("idx %d: expected StaleNaN at t=%d, got %v", i, w.ts, out[i].v)
			}
		} else if math.Float64bits(out[i].v) != math.Float64bits(w.val) {
			t.Errorf("idx %d: expected %v, got %v", i, w.val, out[i].v)
		}
	}
}

func TestPadMissingStepsWithStaleNaN_NoOpWhenFullyCovered(t *testing.T) {
	in := seriesSetOf(
		promapi.FloatSample(0, 1),
		promapi.FloatSample(60_000, 2),
		promapi.FloatSample(120_000, 3),
	)
	out := readPadSeries(t, padMissingStepsWithStaleNaN(in, timestamp.Time(0), timestamp.Time(120_000), time.Minute))
	if len(out) != 3 {
		t.Fatalf("expected 3 values (no padding), got %d", len(out))
	}
}

func TestPadMissingStepsWithStaleNaN_PreservesHistograms(t *testing.T) {
	// A storage.Series carries floats and histograms in one timestamp-ordered
	// stream, so — unlike the original model.Value implementation — a step that
	// already holds a histogram is treated as covered: the histogram blocks the
	// float lookback at its own step, so no float StaleNaN is added there (which
	// would otherwise collide at that timestamp). Histograms pass through
	// untouched.
	hist := &histogram.FloatHistogram{Count: 4, Sum: 5}
	in := promapi.NewSeriesSet([]storage.Series{
		promapi.NewSeries(labels.EmptyLabels(), []chunks.Sample{
			promapi.HistogramSample(0, hist),
			promapi.FloatSample(60_000, 1),
			promapi.HistogramSample(120_000, hist),
		}),
	}, nil, nil)
	out := readPadSeries(t, padMissingStepsWithStaleNaN(in, timestamp.Time(0), timestamp.Time(120_000), time.Minute))
	var nHist, nFloat int
	for _, s := range out {
		if s.isHist {
			nHist++
		} else {
			nFloat++
		}
	}
	if nHist != 2 {
		t.Fatalf("histograms must be preserved unchanged, got %d", nHist)
	}
	// Every step (0=hist, 60s=float, 120s=hist) is covered, so no StaleNaN is
	// added — the single original float is the only float sample.
	if nFloat != 1 {
		t.Fatalf("expected 1 float value (no padding; all steps covered), got %d: %v", nFloat, out)
	}
}

func TestPadMissingStepsWithStaleNaN_IgnoresInstant(t *testing.T) {
	in := seriesSetOf(promapi.FloatSample(0, 1))
	out := readPadSeries(t, padMissingStepsWithStaleNaN(in, timestamp.Time(0), timestamp.Time(0), 0))
	if len(out) != 1 {
		t.Fatalf("interval=0 must be a no-op, got %d values", len(out))
	}
}
