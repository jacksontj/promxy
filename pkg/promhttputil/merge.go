package promhttputil

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/util/annotations"
)

// warningPrefixes are the textual prefixes prometheus uses when formatting
// PromQL annotations through %w wrapping of the sentinel errors (see
// util/annotations/annotations.go). The v1 HTTP/JSON API serialises
// annotations as plain strings, losing the typed wrapping; downstreams that
// re-inflate them (us) have to detect the prefix and re-wrap with the
// matching sentinel so callers can distinguish info-vs-warning via
// errors.Is.
var warningPrefixes = []struct {
	prefix string
	parent error
}{
	{"PromQL warning: ", annotations.PromQLWarning},
	{"PromQL info: ", annotations.PromQLInfo},
}

// annotationPosSuffix matches the " (line:col)" suffix that upstream's
// annotations.AsStrings appends to formatted annotation messages. The
// position refers to the upstream query's text, which is meaningless to
// promxy's callers (we never round-trip the original query string).
// Stripping it also lets the upstream promql test framework match
// expected warning text exactly.
var annotationPosSuffix = regexp.MustCompile(` \(\d+:\d+\)$`)

// WarningsConvert converts v1.Warnings (the JSON-decoded plain-string form
// of an annotation set) to an annotations.Annotations, preserving the
// info-vs-warning classification when the original prefix is present.
func WarningsConvert(ws v1.Warnings) annotations.Annotations {
	a := annotations.New()
	for _, item := range ws {
		a.Add(toAnnotationError(item))
	}
	return *a
}

func toAnnotationError(s string) error {
	s = annotationPosSuffix.ReplaceAllString(s, "")
	for _, p := range warningPrefixes {
		if rest, ok := strings.CutPrefix(s, p.prefix); ok {
			return fmt.Errorf("%w: %s", p.parent, rest)
		}
	}
	return errors.New(s)
}

// WarningSet simply contains a set of warnings
type WarningSet map[string]struct{}

// AddWarnings will add all warnings to the set
func (s WarningSet) AddWarnings(ws v1.Warnings) {
	for _, w := range ws {
		s.AddWarning(w)
	}
}

// AddWarning will add a given warning to the set
func (s WarningSet) AddWarning(w string) {
	s[w] = struct{}{}
}

// Warnings returns all of the warnings contained in the set
func (s WarningSet) Warnings() v1.Warnings {
	w := make(v1.Warnings, 0, len(s))
	for k := range s {
		w = append(w, k)
	}
	return w
}

// MergeSampleStream merges SampleStreams `a` and `b` with the given
// antiAffinityBuffer. When combining series from 2 different prometheus
// hosts we can run into clock-skew / scrape-skew issues (the timestamp
// prometheus stores is the *start* of the scrape, and exporter response
// time varies); refusing to merge any datapoint within antiAffinityBuffer
// of another lets us tolerate antiAffinityBuffer/2 on either side.
//
// When dynamic is true the buffer is recomputed per series from the
// inter-sample spacing of the longer side: half the median gap, modelling
// "scrape interval / 2" without forcing operators to know the interval up
// front. antiAffinityBuffer is the floor / fallback when there are too
// few samples to estimate (< 3 gaps). See #734.
func MergeSampleStream(antiAffinityBuffer model.Time, dynamic bool, a, b *model.SampleStream, preferMax bool) (*model.SampleStream, error) {
	if a.Metric.Fingerprint() != b.Metric.Fingerprint() {
		return nil, fmt.Errorf("cannot merge mismatch fingerprints")
	}

	// Compute the dynamic buffer up front so both the histogram and float
	// merges below see the same per-series estimate. (Done here rather
	// than after the swap-for-longer-side so histograms get the dynamic
	// treatment even when the stream has no float samples at all.)
	if dynamic {
		if dyn, ok := dynamicBufferForStream(a, b); ok {
			antiAffinityBuffer = dyn
		}
	}

	// Float and histogram samples coexist on a SampleStream; merge each
	// sequence independently so a series that carries both still flows
	// through the anti-affinity dedup correctly.
	mergedHistograms := mergeHistogramSamples(antiAffinityBuffer, a.Histograms, b.Histograms)

	// if either set of values are empty, fall back to the side with float
	// data; histograms are merged separately above and re-attached at the end.
	if len(a.Values) == 0 && len(b.Values) == 0 {
		return &model.SampleStream{
			Metric:     a.Metric,
			Histograms: mergedHistograms,
		}, nil
	}
	if len(a.Values) == 0 {
		return &model.SampleStream{
			Metric:     b.Metric,
			Values:     b.Values,
			Histograms: mergedHistograms,
		}, nil
	} else if len(b.Values) == 0 {
		return &model.SampleStream{
			Metric:     a.Metric,
			Values:     a.Values,
			Histograms: mergedHistograms,
		}, nil
	}

	// If B has more points then we want to use that as the base for merging. This is important as
	// the majority of time there are holes in the data a single downstream
	// has a hole but the other has the data; in that case since we have the
	// data in memory there is no reason to chose the "worse" data and merge
	// from there.
	// Note: This has the caveat that this is done on a per-merge basis; so if there
	// are N servers and the first 2 return with holes they will be merged; but
	// due to anti-affinity if there is any server with no hole it will always
	// have more points than a merged series.
	if len(b.Values) > len(a.Values) {
		tmp := a
		a = b
		b = tmp
	}

	newValues := make([]model.SamplePair, 0, len(a.Values))

	bOffset := 0
	aStartBuffered := a.Values[0].Timestamp - antiAffinityBuffer

	// start by loading b points before a
	if b.Values[0].Timestamp < aStartBuffered {
		for i, bValue := range b.Values {
			bOffset = i
			if bValue.Timestamp < aStartBuffered {
				newValues = append(newValues, bValue)
			} else {
				break
			}
		}

	}

	lastOffset := bOffset

	for _, aValue := range a.Values {
		// if we have no points, this one by definition is valid
		if len(newValues) == 0 {
			newValues = append(newValues, aValue)
			continue
		}

		// if there is a gap between the last 2 points > antiAffinityBuffer
		// check if b has a point that would fit in there
		lastTime := newValues[len(newValues)-1].Timestamp
		if (aValue.Timestamp - lastTime) > antiAffinityBuffer*2 {
			// We want to see if we have any datapoints in the window that aren't too close
			for ; bOffset < len(b.Values); bOffset++ {
				bValue := b.Values[bOffset]
				if bValue.Timestamp >= aValue.Timestamp {
					break
				}
				if bValue.Timestamp > lastTime+antiAffinityBuffer && bValue.Timestamp < (aValue.Timestamp-antiAffinityBuffer) {
					newValues = append(newValues, bValue)
				}
			}
		}

		if !preferMax {
			newValues = append(newValues, aValue)
		} else {
			done := false

			// see if there is a sample from b within antiAffinityBuffer, that is larger than a
			for i := lastOffset; i < len(b.Values); i++ {
				bValue := b.Values[i]
				// b is not within antiAffinityBuffer of a
				if bValue.Timestamp >= (aValue.Timestamp+antiAffinityBuffer) && bValue.Timestamp != aValue.Timestamp {
					break
				}
				// b is within antiAffinityBuffer of a
				if bValue.Timestamp == aValue.Timestamp || bValue.Timestamp > (aValue.Timestamp-antiAffinityBuffer) {
					// no need to iterate b before this offset next time
					lastOffset = i
					if bValue.Value > aValue.Value {
						// use the larger value from b
						// note: there may be larger values from b after this, we will choose the first one we find
						// within the antiAffinityBuffer
						bValue.Timestamp = aValue.Timestamp
						newValues = append(newValues, bValue)
						done = true
					}
					break
				}
			}

			if !done {
				//use the larger value from a
				newValues = append(newValues, aValue)
			}
		}
	}

	lastTime := newValues[len(newValues)-1].Timestamp
	for ; bOffset < len(b.Values); bOffset++ {
		bValue := b.Values[bOffset]
		if bValue.Timestamp > lastTime+antiAffinityBuffer {
			newValues = append(newValues, bValue)
		}
	}

	return &model.SampleStream{
		Metric:     a.Metric,
		Values:     newValues,
		Histograms: mergedHistograms,
	}, nil
}

// mergeHistogramSamples mirrors the float anti-affinity dedup in
// MergeSampleStream for histogram samples. preferMax has no defined
// semantics for histograms, so we always prefer the longer stream's sample
// when both sides observe within the buffer window — equivalent to the
// preferMax=false branch of the float merge.
func mergeHistogramSamples(antiAffinityBuffer model.Time, a, b []model.SampleHistogramPair) []model.SampleHistogramPair {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	if len(a) == 0 {
		return b
	}
	if len(b) == 0 {
		return a
	}

	if len(b) > len(a) {
		a, b = b, a
	}

	newValues := make([]model.SampleHistogramPair, 0, len(a))

	bOffset := 0
	aStartBuffered := a[0].Timestamp - antiAffinityBuffer

	if b[0].Timestamp < aStartBuffered {
		for i, bValue := range b {
			bOffset = i
			if bValue.Timestamp < aStartBuffered {
				newValues = append(newValues, bValue)
			} else {
				break
			}
		}
	}

	for _, aValue := range a {
		if len(newValues) == 0 {
			newValues = append(newValues, aValue)
			continue
		}

		lastTime := newValues[len(newValues)-1].Timestamp
		if (aValue.Timestamp - lastTime) > antiAffinityBuffer*2 {
			for ; bOffset < len(b); bOffset++ {
				bValue := b[bOffset]
				if bValue.Timestamp >= aValue.Timestamp {
					break
				}
				if bValue.Timestamp > lastTime+antiAffinityBuffer && bValue.Timestamp < (aValue.Timestamp-antiAffinityBuffer) {
					newValues = append(newValues, bValue)
				}
			}
		}

		newValues = append(newValues, aValue)
	}

	lastTime := newValues[len(newValues)-1].Timestamp
	for ; bOffset < len(b); bOffset++ {
		bValue := b[bOffset]
		if bValue.Timestamp > lastTime+antiAffinityBuffer {
			newValues = append(newValues, bValue)
		}
	}

	return newValues
}

// minDynamicGaps is the smallest number of inter-sample gaps we'll accept
// before trusting a dynamic estimate; below that a single odd gap would
// dominate the median.
const minDynamicGaps = 3

// sampleTimestamp and histogramTimestamp are the timestamp accessors handed
// to dynamicAntiAffinityFrom. They're plain (non-capturing) functions so the
// estimator can walk either sample type without materialising a
// []model.Time copy of the series.
func sampleTimestamp(p model.SamplePair) model.Time { return p.Timestamp }

func histogramTimestamp(p model.SampleHistogramPair) model.Time { return p.Timestamp }

// dynamicAntiAffinity infers a per-series anti-affinity buffer from the
// inter-sample spacing of the longer side. Returns half the median gap and
// ok=true when at least minDynamicGaps gaps are available; returns ok=false
// when there isn't enough data, in which case callers should fall back to
// the configured value.
//
// Median rather than mean: a series that lost a single scrape (gap == 2*
// interval) shouldn't push the estimate toward 1.5*interval. Using half the
// gap models "scrape interval / 2" — the same value the existing static
// `anti_affinity` is documented to want.
func dynamicAntiAffinity(a, b []model.SamplePair) (model.Time, bool) {
	return dynamicAntiAffinityFrom(a, b, sampleTimestamp)
}

// dynamicAntiAffinityFrom is the worker behind dynamicAntiAffinity, generic
// over the sample type so the histogram path (which carries
// SampleHistogramPair, not SamplePair) shares the same estimator. ts reads
// the timestamp out of one sample; nothing else about a sample is touched,
// so neither side is ever copied. This runs per-series on every HA merge, so
// the gaps slice is deliberately the only allocation it makes.
func dynamicAntiAffinityFrom[T any](a, b []T, ts func(T) model.Time) (model.Time, bool) {
	gaps := make([]model.Time, 0, max(len(a)-1, 0))
	gaps = appendGaps(gaps, a, ts)
	// Borrow gaps from b only when a is too short — keeps the estimate
	// rooted in the longer series rather than averaging across two
	// possibly-different scrape rates. a's gaps are kept, not discarded,
	// when we do borrow.
	if len(gaps) < minDynamicGaps {
		gaps = appendGaps(gaps, b, ts)
	}
	if len(gaps) < minDynamicGaps {
		return 0, false
	}
	// gaps holds at most one entry per sample and slices.Sort allocates
	// nothing, so a full sort is cheap enough here; keep it allocation-free
	// (TestDynamicAntiAffinity_Allocs pins the count).
	slices.Sort(gaps)
	median := gaps[len(gaps)/2]
	return median / 2, true
}

// appendGaps appends the deltas between consecutive samples of s. Only
// positive deltas count: duplicate or out-of-order timestamps say nothing
// about the scrape interval and shouldn't drag the median down.
func appendGaps[T any](gaps []model.Time, s []T, ts func(T) model.Time) []model.Time {
	for i := 1; i < len(s); i++ {
		if d := ts(s[i]) - ts(s[i-1]); d > 0 {
			gaps = append(gaps, d)
		}
	}
	return gaps
}

// dynamicBufferForStream picks the dynamic buffer for a SampleStream pair.
// It first tries to estimate from the float samples (anchored on whichever
// side has more data); if that's too short, it falls back to estimating
// from the histogram samples. Returns ok=false when neither sample type
// provides enough gaps, in which case callers should keep the configured
// value.
func dynamicBufferForStream(a, b *model.SampleStream) (model.Time, bool) {
	fa, fb := a.Values, b.Values
	if len(fb) > len(fa) {
		fa, fb = fb, fa
	}
	if dyn, ok := dynamicAntiAffinity(fa, fb); ok {
		return dyn, true
	}

	ha, hb := a.Histograms, b.Histograms
	if len(hb) > len(ha) {
		ha, hb = hb, ha
	}
	return dynamicAntiAffinityFrom(ha, hb, histogramTimestamp)
}
