package promclient

import (
	"math"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/value"
)

// InjectStaleMarkers walks each series in v (when v is a model.Matrix) and
// inserts a stale-NaN marker at the first step following every "gap" between
// real samples. A gap is any pair of consecutive samples whose timestamps
// differ by more than stepMs; we also treat the end of each series as a
// gap so a stale marker is appended one step past the last real sample.
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
// stepMs must be > 0; otherwise the matrix is returned unchanged.
func InjectStaleMarkers(v model.Value, stepMs int64) model.Value {
	if stepMs <= 0 {
		return v
	}
	matrix, ok := v.(model.Matrix)
	if !ok {
		return v
	}
	staleF := math.Float64frombits(value.StaleNaN)
	staleSample := model.SamplePair{Value: model.SampleValue(staleF)}
	for _, stream := range matrix {
		stream.Values = injectStaleIntoFloats(stream.Values, stepMs, staleSample)
		stream.Histograms = injectStaleIntoHistograms(stream.Histograms, stepMs, staleF)
	}
	return matrix
}

// injectStaleIntoFloats returns a new slice with stale markers inserted in
// any gap between consecutive timestamps that exceeds stepMs, plus one
// marker one step past the last real sample so a trailing gap also
// short-circuits the engine's lookback.
func injectStaleIntoFloats(in []model.SamplePair, stepMs int64, stale model.SamplePair) []model.SamplePair {
	if len(in) == 0 {
		return in
	}
	out := make([]model.SamplePair, 0, len(in)+1)
	for i, s := range in {
		out = append(out, s)
		next := int64(s.Timestamp) + stepMs
		if i+1 < len(in) {
			if int64(in[i+1].Timestamp) > next {
				out = append(out, model.SamplePair{Timestamp: model.Time(next), Value: stale.Value})
			}
			continue
		}
		// Trailing marker after the final real sample.
		out = append(out, model.SamplePair{Timestamp: model.Time(next), Value: stale.Value})
	}
	return out
}

// injectStaleIntoHistograms mirrors injectStaleIntoFloats for the histogram
// channel of a SampleStream. The marker is a SampleHistogram whose Sum is
// StaleNaN — promql/engine recognises that as a stale-histogram boundary
// (see engine.vectorSelectorSingle).
func injectStaleIntoHistograms(in []model.SampleHistogramPair, stepMs int64, staleF float64) []model.SampleHistogramPair {
	if len(in) == 0 {
		return in
	}
	out := make([]model.SampleHistogramPair, 0, len(in)+1)
	for i, s := range in {
		out = append(out, s)
		next := int64(s.Timestamp) + stepMs
		marker := model.SampleHistogramPair{
			Timestamp: model.Time(next),
			Histogram: &model.SampleHistogram{Sum: model.FloatString(staleF)},
		}
		if i+1 < len(in) {
			if int64(in[i+1].Timestamp) > next {
				out = append(out, marker)
			}
			continue
		}
		out = append(out, marker)
	}
	return out
}
