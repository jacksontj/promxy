package promclient

import (
	"testing"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// pt is a flattened sample read back from a storage.Series for assertions.
type pt struct {
	t       int64
	v       float64
	isHist  bool
	histSum float64
}

// readSeries materializes the (single-series) SeriesSet into its samples.
func readSeries(t *testing.T, ss storage.SeriesSet) []pt {
	t.Helper()
	var series [][]pt
	for ss.Next() {
		var pts []pt
		it := ss.At().Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				ts, v := it.At()
				pts = append(pts, pt{t: ts, v: v})
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				ts, fh := it.AtFloatHistogram(nil)
				pts = append(pts, pt{t: ts, isHist: true, histSum: fh.Sum})
			}
		}
		if err := it.Err(); err != nil {
			t.Fatalf("iterator error: %v", err)
		}
		series = append(series, pts)
	}
	if err := ss.Err(); err != nil {
		t.Fatalf("series set error: %v", err)
	}
	if len(series) == 0 {
		return nil
	}
	return series[0]
}

func floatSeriesSet(samples ...chunks.Sample) storage.SeriesSet {
	return promapi.NewSeriesSet(
		[]storage.Series{promapi.NewSeries(labels.FromStrings("job", "a"), samples)},
		nil, nil,
	)
}

func TestInjectStaleMarkersNoGap(t *testing.T) {
	// Contiguous samples at step boundaries get no stale marker between
	// pairs, only a trailing marker one step past the last sample.
	in := floatSeriesSet(
		promapi.FloatSample(0, 1),
		promapi.FloatSample(1000, 2),
		promapi.FloatSample(2000, 3),
	)
	out := readSeries(t, InjectStaleMarkers(in, 1000))
	if got := len(out); got != 4 {
		t.Fatalf("expected 4 samples (3 real + 1 trailing stale), got %d", got)
	}
	for i, want := range []int64{0, 1000, 2000, 3000} {
		if out[i].t != want {
			t.Errorf("sample[%d] timestamp = %d, want %d", i, out[i].t, want)
		}
	}
	if !value.IsStaleNaN(out[3].v) {
		t.Errorf("trailing sample at t=3000 should be a stale marker")
	}
	// Real samples must not have been converted to stale.
	for i := 0; i < 3; i++ {
		if value.IsStaleNaN(out[i].v) {
			t.Errorf("real sample[%d] was overwritten with a stale marker", i)
		}
	}
}

func TestInjectStaleMarkersWithGap(t *testing.T) {
	// A gap of more than one step between samples gets exactly one stale
	// marker at the first step after the earlier sample.
	in := floatSeriesSet(
		promapi.FloatSample(0, 1),
		promapi.FloatSample(5000, 2),
	)
	out := readSeries(t, InjectStaleMarkers(in, 1000))
	// Expect 4 entries: real(0), stale(1000), real(5000), trailing-stale(6000).
	if got := len(out); got != 4 {
		t.Fatalf("expected 4 samples, got %d", got)
	}
	if out[1].t != 1000 || !value.IsStaleNaN(out[1].v) {
		t.Errorf("expected stale marker at t=1000, got ts=%d value=%v", out[1].t, out[1].v)
	}
	if out[2].t != 5000 || value.IsStaleNaN(out[2].v) {
		t.Errorf("expected real sample at t=5000, got ts=%d value=%v", out[2].t, out[2].v)
	}
	if out[3].t != 6000 || !value.IsStaleNaN(out[3].v) {
		t.Errorf("expected trailing stale at t=6000, got ts=%d value=%v", out[3].t, out[3].v)
	}
}

func TestInjectStaleMarkersEmptyOrInvalid(t *testing.T) {
	if got := readSeries(t, InjectStaleMarkers(promapi.NewSeriesSet(nil, nil, nil), 1000)); len(got) != 0 {
		t.Errorf("empty set should round-trip unchanged")
	}
	// stepMs <= 0 must be a no-op even on real data, so callers that
	// accidentally pass an instant-query result don't get bogus markers.
	in := floatSeriesSet(promapi.FloatSample(0, 1))
	out := readSeries(t, InjectStaleMarkers(in, 0))
	if len(out) != 1 {
		t.Errorf("zero step should not inject markers; got %d samples", len(out))
	}
}

func TestInjectStaleMarkersHistograms(t *testing.T) {
	in := promapi.NewSeriesSet([]storage.Series{
		promapi.NewSeries(labels.FromStrings("job", "a"), []chunks.Sample{
			promapi.HistogramSample(0, &histogram.FloatHistogram{Count: 1, Sum: 1}),
			promapi.HistogramSample(5000, &histogram.FloatHistogram{Count: 2, Sum: 2}),
		}),
	}, nil, nil)
	out := readSeries(t, InjectStaleMarkers(in, 1000))
	// Same structure as the float case: 2 real + 1 mid-gap stale + 1 trailing.
	if got := len(out); got != 4 {
		t.Fatalf("expected 4 histogram samples, got %d", got)
	}
	if out[1].t != 1000 || !out[1].isHist || !value.IsStaleNaN(out[1].histSum) {
		t.Errorf("expected histogram stale marker (Sum=StaleNaN) at t=1000, got %+v", out[1])
	}
	if out[2].t != 5000 || value.IsStaleNaN(out[2].histSum) {
		t.Errorf("expected real histogram at t=5000, got %+v", out[2])
	}
	if out[3].t != 6000 || !value.IsStaleNaN(out[3].histSum) {
		t.Errorf("expected trailing histogram stale at t=6000, got %+v", out[3])
	}
}
