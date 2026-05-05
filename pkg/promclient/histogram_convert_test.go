package promclient

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// TestFloatHistogramRoundTripPreservesSchema is the canary that proves the
// remote_read fanout path keeps full FloatHistogram fidelity. The forward
// converter produces a model.SampleHistogram and pins the original; the
// reverse converter must hand back the same pointer (or at least an
// equivalent FloatHistogram), not the lossy custom-buckets reconstruction.
func TestFloatHistogramRoundTripPreservesSchema(t *testing.T) {
	original := &histogram.FloatHistogram{
		Schema:          histogram.CustomBucketsSchema,
		Count:           4,
		Sum:             2.5,
		CustomValues:    []float64{0, 1, 2},
		PositiveSpans:   []histogram.Span{{Offset: 0, Length: 4}},
		PositiveBuckets: []float64{1, 0, 0, 0},
	}

	sh := floatHistogramToSampleHistogram(original)
	if sh == nil {
		t.Fatal("forward conversion returned nil")
	}

	roundTripped := sampleHistogramToFloatHistogram(sh)
	if roundTripped == nil {
		t.Fatal("reverse conversion returned nil")
	}

	// Pinned path must hand back the exact same FloatHistogram pointer.
	if roundTripped != original {
		t.Fatalf("pin lost: expected the original pointer back, got reconstructed.\n"+
			"original=%+v\nroundTripped=%+v", original, roundTripped)
	}

	// And the pinned histogram must still preserve empty buckets.
	if len(roundTripped.PositiveBuckets) != 4 {
		t.Fatalf("expected 4 PositiveBuckets, got %d", len(roundTripped.PositiveBuckets))
	}
	if len(roundTripped.CustomValues) != 3 {
		t.Fatalf("expected 3 CustomValues, got %d", len(roundTripped.CustomValues))
	}
}

// TestSampleHistogramFallbackReconstruction exercises the lossy custom-buckets
// path that runs when no original FloatHistogram is pinned (the HTTP-API
// fanout case). It must produce a usable FloatHistogram even though empty
// buckets are absent.
func TestSampleHistogramFallbackReconstruction(t *testing.T) {
	sh := &model.SampleHistogram{
		Count: 5,
		Sum:   12.5,
		Buckets: model.HistogramBuckets{
			{Boundaries: 0, Lower: 0, Upper: 1, Count: 2},
			{Boundaries: 0, Lower: 1, Upper: 2, Count: 3},
		},
	}
	fh := sampleHistogramToFloatHistogram(sh)
	if fh == nil {
		t.Fatal("reverse conversion returned nil for non-pinned histogram")
	}
	if fh.Schema != histogram.CustomBucketsSchema {
		t.Fatalf("expected CustomBucketsSchema, got %d", fh.Schema)
	}
	if got, want := fh.Count, 5.0; got != want {
		t.Fatalf("count: got %v want %v", got, want)
	}
	if got, want := fh.CustomValues, []float64{1, 2}; !equal(got, want) {
		t.Fatalf("CustomValues: got %v want %v", got, want)
	}
}

// TestSeriesIteratorEmitsHistogramSamples checks that a SeriesIterator wrapping
// a model.SampleStream with histogram samples actually produces them via Next +
// AtFloatHistogram. This catches regressions where the iterator drops
// histograms before the engine can see them.
func TestSeriesIteratorEmitsHistogramSamples(t *testing.T) {
	original := &histogram.FloatHistogram{
		Schema:          histogram.CustomBucketsSchema,
		Count:           4,
		Sum:             2.5,
		CustomValues:    []float64{0, 1, 2},
		PositiveSpans:   []histogram.Span{{Offset: 0, Length: 4}},
		PositiveBuckets: []float64{1, 0, 0, 0},
	}
	stream := &model.SampleStream{
		Metric: model.Metric{model.MetricNameLabel: "h"},
		Histograms: []model.SampleHistogramPair{
			{Timestamp: 100, Histogram: floatHistogramToSampleHistogram(original)},
			{Timestamp: 200, Histogram: floatHistogramToSampleHistogram(original)},
		},
	}
	it := NewSeriesIterator(stream)
	count := 0
	for {
		vt := it.Next()
		if vt == chunkenc.ValNone {
			break
		}
		if vt != chunkenc.ValFloatHistogram {
			t.Fatalf("expected ValFloatHistogram, got %v", vt)
		}
		_, fh := it.AtFloatHistogram(nil)
		if fh == nil {
			t.Fatal("AtFloatHistogram returned nil despite pinned histogram")
		}
		if len(fh.PositiveBuckets) != 4 {
			t.Fatalf("expected 4 PositiveBuckets via iterator, got %d", len(fh.PositiveBuckets))
		}
		count++
	}
	if count != 2 {
		t.Fatalf("expected 2 histogram samples, got %d", count)
	}
}

// TestSeriesIteratorEmitsConstHistogramSequence simulates the const_histogram
// scenario from histograms.test: every sample is the same FloatHistogram
// (most buckets empty). The engine reads the series with a non-nil hint that
// it reuses across Next() calls; the iterator must hand back deep copies that
// each preserve the full CustomValues schema, so a downstream rate() doesn't
// see a degraded input that yields an empty-schema result.
func TestSeriesIteratorEmitsConstHistogramSequence(t *testing.T) {
	original := &histogram.FloatHistogram{
		Schema:          histogram.CustomBucketsSchema,
		Count:           1,
		Sum:             0,
		CustomValues:    []float64{0, 1, 2},
		PositiveSpans:   []histogram.Span{{Offset: 0, Length: 4}},
		PositiveBuckets: []float64{1, 0, 0, 0},
	}
	stream := &model.SampleStream{
		Metric: model.Metric{model.MetricNameLabel: "h"},
		Histograms: []model.SampleHistogramPair{
			{Timestamp: 60_000, Histogram: floatHistogramToSampleHistogram(original)},
			{Timestamp: 120_000, Histogram: floatHistogramToSampleHistogram(original)},
			{Timestamp: 180_000, Histogram: floatHistogramToSampleHistogram(original)},
		},
	}
	it := NewSeriesIterator(stream)
	hint := &histogram.FloatHistogram{}
	count := 0
	for {
		vt := it.Next()
		if vt == chunkenc.ValNone {
			break
		}
		_, fh := it.AtFloatHistogram(hint)
		if fh != hint {
			t.Fatalf("sample %d: expected iterator to return the hint pointer back, got a different pointer", count)
		}
		if got, want := fh.CustomValues, []float64{0, 1, 2}; !equal(got, want) {
			t.Fatalf("sample %d: CustomValues lost on copy: got %v want %v", count, got, want)
		}
		if got, want := fh.PositiveBuckets, []float64{1, 0, 0, 0}; !equal(got, want) {
			t.Fatalf("sample %d: PositiveBuckets lost on copy: got %v want %v", count, got, want)
		}
		count++
	}
	if count != 3 {
		t.Fatalf("expected 3 samples, got %d", count)
	}
}

func equal(a, b []float64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
