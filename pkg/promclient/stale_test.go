package promclient

import (
	"math"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/value"
)

func TestInjectStaleMarkersNoGap(t *testing.T) {
	// Contiguous samples at step boundaries get no stale marker between
	// pairs, only a trailing marker one step past the last sample.
	m := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"job": "a"},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 1000, Value: 2},
				{Timestamp: 2000, Value: 3},
			},
		},
	}
	out := InjectStaleMarkers(m, 1000).(model.Matrix)
	if got := len(out[0].Values); got != 4 {
		t.Fatalf("expected 4 samples (3 real + 1 trailing stale), got %d", got)
	}
	for i, want := range []int64{0, 1000, 2000, 3000} {
		if int64(out[0].Values[i].Timestamp) != want {
			t.Errorf("sample[%d] timestamp = %d, want %d", i, int64(out[0].Values[i].Timestamp), want)
		}
	}
	if !value.IsStaleNaN(float64(out[0].Values[3].Value)) {
		t.Errorf("trailing sample at t=3000 should be a stale marker")
	}
	// Real samples must not have been converted to stale.
	for i := 0; i < 3; i++ {
		if value.IsStaleNaN(float64(out[0].Values[i].Value)) {
			t.Errorf("real sample[%d] was overwritten with a stale marker", i)
		}
	}
}

func TestInjectStaleMarkersWithGap(t *testing.T) {
	// A gap of more than one step between samples gets exactly one stale
	// marker at the first step after the earlier sample.
	m := model.Matrix{
		&model.SampleStream{
			Metric: model.Metric{"job": "a"},
			Values: []model.SamplePair{
				{Timestamp: 0, Value: 1},
				{Timestamp: 5000, Value: 2},
			},
		},
	}
	out := InjectStaleMarkers(m, 1000).(model.Matrix)
	// Expect 4 entries: real(0), stale(1000), real(5000), trailing-stale(6000).
	if got := len(out[0].Values); got != 4 {
		t.Fatalf("expected 4 samples, got %d", got)
	}
	if int64(out[0].Values[1].Timestamp) != 1000 || !value.IsStaleNaN(float64(out[0].Values[1].Value)) {
		t.Errorf("expected stale marker at t=1000, got ts=%d value=%v", int64(out[0].Values[1].Timestamp), float64(out[0].Values[1].Value))
	}
	if int64(out[0].Values[2].Timestamp) != 5000 || value.IsStaleNaN(float64(out[0].Values[2].Value)) {
		t.Errorf("expected real sample at t=5000, got ts=%d value=%v", int64(out[0].Values[2].Timestamp), float64(out[0].Values[2].Value))
	}
	if int64(out[0].Values[3].Timestamp) != 6000 || !value.IsStaleNaN(float64(out[0].Values[3].Value)) {
		t.Errorf("expected trailing stale at t=6000, got ts=%d value=%v", int64(out[0].Values[3].Timestamp), float64(out[0].Values[3].Value))
	}
}

func TestInjectStaleMarkersEmptyOrInvalid(t *testing.T) {
	if got := InjectStaleMarkers(model.Matrix{}, 1000); len(got.(model.Matrix)) != 0 {
		t.Errorf("empty matrix should round-trip unchanged")
	}
	// stepMs <= 0 must be a no-op even on real data, so callers that
	// accidentally pass an instant-query result don't get bogus markers.
	m := model.Matrix{
		&model.SampleStream{Values: []model.SamplePair{{Timestamp: 0, Value: 1}}},
	}
	out := InjectStaleMarkers(m, 0).(model.Matrix)
	if len(out[0].Values) != 1 {
		t.Errorf("zero step should not inject markers; got %d samples", len(out[0].Values))
	}
	// Non-matrix values pass through.
	v := &model.Scalar{Timestamp: 5, Value: 1}
	if got := InjectStaleMarkers(v, 1000); got != v {
		t.Errorf("non-matrix should pass through unchanged")
	}
}

func TestInjectStaleMarkersHistograms(t *testing.T) {
	m := model.Matrix{
		&model.SampleStream{
			Histograms: []model.SampleHistogramPair{
				{Timestamp: 0, Histogram: &model.SampleHistogram{Count: 1, Sum: 1}},
				{Timestamp: 5000, Histogram: &model.SampleHistogram{Count: 2, Sum: 2}},
			},
		},
	}
	out := InjectStaleMarkers(m, 1000).(model.Matrix)
	// Same structure as float case: 2 real + 1 mid-gap stale + 1 trailing.
	if got := len(out[0].Histograms); got != 4 {
		t.Fatalf("expected 4 histogram samples, got %d", got)
	}
	if int64(out[0].Histograms[1].Timestamp) != 1000 || !math.IsNaN(float64(out[0].Histograms[1].Histogram.Sum)) {
		t.Errorf("expected histogram stale at t=1000, got %+v", out[0].Histograms[1])
	}
	if !value.IsStaleNaN(float64(out[0].Histograms[1].Histogram.Sum)) {
		t.Errorf("mid-gap histogram Sum should be StaleNaN bits, got %x", math.Float64bits(float64(out[0].Histograms[1].Histogram.Sum)))
	}
}
