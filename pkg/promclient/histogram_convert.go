package promclient

import (
	"math"
	"runtime"
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
)

// Promxy carries native histograms through a model.SampleHistogram-shaped
// carrier (the JSON API representation) so that fan-out merging, dedup, and
// JSON pass-through can reuse the existing model.Value-based plumbing. The
// JSON shape is lossy (it drops empty buckets and the sparse-span schema),
// so when the upstream is a remote_read source that natively produces a
// histogram.FloatHistogram, we additionally pin the original FloatHistogram
// in a pointer-keyed side channel. The iterator's AtFloatHistogram consults
// this channel before falling back to the best-effort reconstructor.
//
// Entries are removed via a finalizer when the SampleHistogram is GC'd,
// which happens once the engine releases the model.Matrix.
var (
	rawHistograms   sync.Map // map[*model.SampleHistogram]*histogram.FloatHistogram
	rawHistogramsMu sync.Mutex
)

// pinFloatHistogram associates fh with sh so a later AtFloatHistogram call
// can return the high-fidelity histogram instead of lossily reconstructing
// from the SampleHistogram.
func pinFloatHistogram(sh *model.SampleHistogram, fh *histogram.FloatHistogram) {
	if sh == nil || fh == nil {
		return
	}
	rawHistograms.Store(sh, fh)
	runtime.SetFinalizer(sh, func(p *model.SampleHistogram) {
		rawHistograms.Delete(p)
	})
}

// pinnedFloatHistogram returns the histogram.FloatHistogram previously
// associated with sh via pinFloatHistogram, or nil if none is pinned.
func pinnedFloatHistogram(sh *model.SampleHistogram) *histogram.FloatHistogram {
	if sh == nil {
		return nil
	}
	if v, ok := rawHistograms.Load(sh); ok {
		return v.(*histogram.FloatHistogram)
	}
	return nil
}

// floatHistogramToSampleHistogram renders a FloatHistogram into the
// API-facing model.SampleHistogram (flat bucket list) so it can be carried
// through promxy's existing model.Value-based merge and HTTP response paths.
// Mirrors prometheus/util/jsonutil.MarshalHistogram (which streams the same
// shape directly to JSON). The original FloatHistogram is pinned so the
// iterator can read it back without losing schema fidelity.
func floatHistogramToSampleHistogram(h *histogram.FloatHistogram) *model.SampleHistogram {
	if h == nil {
		return nil
	}
	sh := &model.SampleHistogram{
		Count: model.FloatString(h.Count),
		Sum:   model.FloatString(h.Sum),
	}
	it := h.AllBucketIterator()
	for it.Next() {
		b := it.At()
		if b.Count == 0 {
			continue
		}
		boundaries := int32(2) // both exclusive
		if b.LowerInclusive {
			if b.UpperInclusive {
				boundaries = 3
			} else {
				boundaries = 1
			}
		} else if b.UpperInclusive {
			boundaries = 0
		}
		sh.Buckets = append(sh.Buckets, &model.HistogramBucket{
			Boundaries: boundaries,
			Lower:      model.FloatString(b.Lower),
			Upper:      model.FloatString(b.Upper),
			Count:      model.FloatString(b.Count),
		})
	}
	pinFloatHistogram(sh, h)
	return sh
}

// sampleHistogramToFloatHistogram is the reverse of floatHistogramToSampleHistogram.
//
// If a FloatHistogram was pinned alongside sh by an earlier
// floatHistogramToSampleHistogram call, return that — this preserves full
// schema fidelity for the remote_read fanout path.
//
// Otherwise, fall back to a best-effort reconstruction as a custom-buckets
// FloatHistogram (Schema=-53). That handles NHCB-shaped histograms but
// drops empty buckets (which the JSON form does not preserve), so quantile
// and stddev/stdvar that depend on those buckets may diverge from a direct
// upstream query.
func sampleHistogramToFloatHistogram(sh *model.SampleHistogram) *histogram.FloatHistogram {
	if sh == nil {
		return nil
	}
	if fh := pinnedFloatHistogram(sh); fh != nil {
		return fh
	}
	fh := &histogram.FloatHistogram{
		Schema: histogram.CustomBucketsSchema,
		Count:  float64(sh.Count),
		Sum:    float64(sh.Sum),
	}
	if len(sh.Buckets) == 0 {
		return fh
	}
	// CustomValues holds the bucket boundaries in strictly-increasing order.
	// For N contiguous buckets we need N+1 values when the first bucket's
	// lower bound is finite (the upper bounds give us N, and the very first
	// lower closes the open end on the left); otherwise N values is correct
	// (the leftmost bound is -Inf and isn't represented).
	//
	// PositiveSpans.Offset shifts where the bucket counts line up against
	// CustomValues: buckets at idx [offset, offset+length) map to bounds
	// (CustomValues[idx-1], CustomValues[idx]]. When we include the first
	// lower we offset by 1 so buckets start at index 1 (the second bound).
	customValues := make([]float64, 0, len(sh.Buckets)+1)
	counts := make([]float64, 0, len(sh.Buckets))
	firstLower := float64(sh.Buckets[0].Lower)
	includeFirstLower := !math.IsInf(firstLower, -1)
	if includeFirstLower {
		customValues = append(customValues, firstLower)
	}
	for _, b := range sh.Buckets {
		upper := float64(b.Upper)
		// +Inf is the implicit final bucket of a custom-buckets histogram;
		// don't list it as an explicit boundary, but keep its count.
		if !math.IsInf(upper, 1) {
			customValues = append(customValues, upper)
		}
		counts = append(counts, float64(b.Count))
	}
	offset := int32(0)
	if includeFirstLower {
		offset = 1
	}
	fh.CustomValues = customValues
	fh.PositiveBuckets = counts
	fh.PositiveSpans = []histogram.Span{{Offset: offset, Length: uint32(len(counts))}}
	return fh
}

// rawHistogramsMu is reserved for future per-querier scoping if the global
// cache becomes a bottleneck under heavy concurrent histogram traffic. The
// finalizer-driven cleanup is sufficient for typical promxy workloads.
var _ = rawHistogramsMu

// TestPinFloatHistogramRoundTrip is exposed as a package-level helper for
// histogram_convert_test.go.
var _ = pinFloatHistogram
