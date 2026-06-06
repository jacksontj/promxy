package proxystorage

import (
	"math"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
)

func TestPadMissingStepsWithStaleNaN_FillsHistogramHoles(t *testing.T) {
	// Mirrors the functions.test mixed_metric case for round / clamp pushdown:
	// upstream returned a Matrix with floats at t=60s,120s,180s (the
	// histogram-bearing 0s/240s/300s steps were dropped by the function),
	// and we want padding to drop a StaleNaN at every missing step in
	// [0s, 300s] so the engine's PeekPrev fallback can't extend the last
	// float across the gaps.
	mat := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{},
			Values: []model.SamplePair{
				{Timestamp: 60_000, Value: 1},
				{Timestamp: 120_000, Value: 2},
				{Timestamp: 180_000, Value: 3},
			},
		},
	}
	start := timestamp.Time(0)
	end := timestamp.Time(300_000)
	out := padMissingStepsWithStaleNaN(model.Value(mat), start, end, time.Minute)
	got, ok := out.(model.Matrix)
	if !ok {
		t.Fatalf("expected Matrix, got %T", out)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 series, got %d", len(got))
	}
	want := []struct {
		ts     model.Time
		stale  bool
		val    float64
	}{
		{0, true, 0},
		{60_000, false, 1},
		{120_000, false, 2},
		{180_000, false, 3},
		{240_000, true, 0},
		{300_000, true, 0},
	}
	if len(got[0].Values) != len(want) {
		t.Fatalf("expected %d values, got %d (%v)", len(want), len(got[0].Values), got[0].Values)
	}
	for i, w := range want {
		v := got[0].Values[i]
		if v.Timestamp != w.ts {
			t.Errorf("idx %d: ts=%d want %d", i, v.Timestamp, w.ts)
		}
		if w.stale {
			if !value.IsStaleNaN(float64(v.Value)) {
				t.Errorf("idx %d: expected StaleNaN at t=%d, got %v", i, w.ts, v.Value)
			}
		} else {
			if math.Float64bits(float64(v.Value)) != math.Float64bits(w.val) {
				t.Errorf("idx %d: expected %v, got %v", i, w.val, v.Value)
			}
		}
	}
}

func TestPadMissingStepsWithStaleNaN_NoOpWhenFullyCovered(t *testing.T) {
	mat := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 60_000, Value: 2},
				{Timestamp: 120_000, Value: 3},
			},
		},
	}
	start := timestamp.Time(0)
	end := timestamp.Time(120_000)
	out := padMissingStepsWithStaleNaN(model.Value(mat), start, end, time.Minute)
	got := out.(model.Matrix)
	if len(got[0].Values) != 3 {
		t.Fatalf("expected 3 values (no padding), got %d", len(got[0].Values))
	}
}

func TestPadMissingStepsWithStaleNaN_PreservesHistograms(t *testing.T) {
	hist := &model.SampleHistogram{
		Count: 4, Sum: 5,
		Buckets: []*model.HistogramBucket{{Boundaries: 0, Lower: 0, Upper: 1, Count: 1}},
	}
	mat := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{},
			Values: []model.SamplePair{
				{Timestamp: 60_000, Value: 1},
			},
			Histograms: []model.SampleHistogramPair{
				{Timestamp: 0, Histogram: hist},
				{Timestamp: 120_000, Histogram: hist},
			},
		},
	}
	out := padMissingStepsWithStaleNaN(model.Value(mat), timestamp.Time(0), timestamp.Time(120_000), time.Minute)
	got := out.(model.Matrix)
	if len(got[0].Histograms) != 2 {
		t.Fatalf("histograms must be preserved unchanged, got %d", len(got[0].Histograms))
	}
	// Pad still adds a stale at t=0 and t=120000 since the float side has gaps there.
	if len(got[0].Values) != 3 {
		t.Fatalf("expected 3 float values (original 1 + 2 stale), got %d: %v", len(got[0].Values), got[0].Values)
	}
}

func TestPadMissingStepsWithStaleNaN_IgnoresInstant(t *testing.T) {
	mat := model.Matrix{
		&model.SampleStream{Metric: model.Metric{}, Values: []model.SamplePair{{Timestamp: 0, Value: 1}}},
	}
	out := padMissingStepsWithStaleNaN(model.Value(mat), timestamp.Time(0), timestamp.Time(0), 0)
	got := out.(model.Matrix)
	if len(got[0].Values) != 1 {
		t.Fatalf("interval=0 must be a no-op, got %d values", len(got[0].Values))
	}
}
