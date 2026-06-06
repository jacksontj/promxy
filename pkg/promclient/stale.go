package promclient

import (
	"math"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// InjectStaleMarkers materializes ss and, for each series, inserts a stale-NaN
// marker at the first step following every "gap" between real samples. A gap is
// any pair of consecutive samples whose timestamps differ by more than stepMs;
// the end of each series is also treated as a gap so a stale marker is appended
// one step past the last real sample.
//
// Background: promxy pushes down range queries (rate, resets, changes,
// delta, …) by issuing QueryRange against an upstream with the engine's
// step. The response is a series of (ts, value) pairs at step boundaries,
// where each value is the function's evaluation at that step. promxy then
// hands these back to the embedded engine wrapped in a VectorSelector and
// the engine re-evaluates the expression at each step of the outer range.
//
// The engine's per-step sample lookup uses the engine-wide lookback
// (5 minutes by default), NOT the VectorSelector's LookbackDelta — which
// only widens the storage fetch window. The result: when a pushed-down
// step produced no value (e.g. resets() on a single-sample range), the
// engine's next step at refTime falls back to the most recent prior
// sample, finds it within the default 5-minute lookback, and emits a
// spurious value for that step.
//
// Injecting a stale marker at the step boundary after each gap forces
// the engine's lookback to short-circuit on the stale and report no
// sample — matching what direct upstream evaluation would do.
//
// stepMs must be > 0; otherwise ss is returned unchanged. The returned set is
// a fresh, re-iterable copy: the source cursor is consumed and every sample is
// copied, so the result is safe to hand on to UnexpandedSeriesSet.
func InjectStaleMarkers(ss storage.SeriesSet, stepMs int64) storage.SeriesSet {
	if stepMs <= 0 {
		return ss
	}
	staleF := math.Float64frombits(value.StaleNaN)
	var out []storage.Series
	for ss.Next() {
		s := ss.At()
		lbls := s.Labels().Copy()

		// Split into float and histogram channels (each already
		// timestamp-ordered) so gaps are detected per channel, matching the
		// engine's separate float/histogram per-step lookback.
		var floats, hists []chunks.Sample
		it := s.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				t, v := it.At()
				floats = append(floats, promapi.FloatSample(t, v))
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				t, fh := it.AtFloatHistogram(nil)
				hists = append(hists, promapi.HistogramSample(t, fh))
			}
		}
		if err := it.Err(); err != nil {
			return promapi.NewSeriesSet(nil, ss.Warnings(), err)
		}

		floats = injectStaleIntoFloats(floats, stepMs, staleF)
		hists = injectStaleIntoHistograms(hists, stepMs, staleF)
		out = append(out, promapi.NewSeries(lbls, mergeSamplesByTime(floats, hists)))
	}
	return promapi.NewSeriesSet(out, ss.Warnings(), ss.Err())
}

// injectStaleIntoFloats returns a new slice with stale markers inserted in
// any gap between consecutive timestamps that exceeds stepMs, plus one
// marker one step past the last real sample so a trailing gap also
// short-circuits the engine's lookback.
func injectStaleIntoFloats(in []chunks.Sample, stepMs int64, staleF float64) []chunks.Sample {
	if len(in) == 0 {
		return in
	}
	out := make([]chunks.Sample, 0, len(in)+1)
	for i, s := range in {
		out = append(out, s)
		next := s.T() + stepMs
		if i+1 < len(in) {
			if in[i+1].T() > next {
				out = append(out, promapi.FloatSample(next, staleF))
			}
			continue
		}
		// Trailing marker after the final real sample.
		out = append(out, promapi.FloatSample(next, staleF))
	}
	return out
}

// injectStaleIntoHistograms mirrors injectStaleIntoFloats for the histogram
// channel. The marker is a FloatHistogram whose Sum is StaleNaN —
// promql/engine recognises that as a stale-histogram boundary (see
// engine.vectorSelectorSingle).
func injectStaleIntoHistograms(in []chunks.Sample, stepMs int64, staleF float64) []chunks.Sample {
	if len(in) == 0 {
		return in
	}
	out := make([]chunks.Sample, 0, len(in)+1)
	for i, s := range in {
		out = append(out, s)
		next := s.T() + stepMs
		marker := promapi.HistogramSample(next, &histogram.FloatHistogram{Sum: staleF})
		if i+1 < len(in) {
			if in[i+1].T() > next {
				out = append(out, marker)
			}
			continue
		}
		out = append(out, marker)
	}
	return out
}

// mergeSamplesByTime merges two timestamp-ordered sample slices into one
// ordered slice. promapi.NewSeries' list iterator must walk samples in
// timestamp order; the float and histogram channels are each ordered but
// interleave by timestamp once stale markers land between them.
func mergeSamplesByTime(a, b []chunks.Sample) []chunks.Sample {
	if len(b) == 0 {
		return a
	}
	if len(a) == 0 {
		return b
	}
	out := make([]chunks.Sample, 0, len(a)+len(b))
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		if a[i].T() <= b[j].T() {
			out = append(out, a[i])
			i++
		} else {
			out = append(out, b[j])
			j++
		}
	}
	out = append(out, a[i:]...)
	out = append(out, b[j:]...)
	return out
}
