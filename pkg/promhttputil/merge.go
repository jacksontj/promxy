package promhttputil

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
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

// ValueAddLabelSet adds the labelset `l` to the value `a`
func ValueAddLabelSet(a model.Value, l model.LabelSet) error {
	switch aTyped := a.(type) {
	case model.Vector:
		for _, item := range aTyped {
			for k, v := range l {
				item.Metric[k] = v
			}
		}

	case model.Matrix:
		for _, item := range aTyped {
			// If the current metric has no labels, set them
			if item.Metric == nil {
				item.Metric = model.Metric(model.LabelSet(make(map[model.LabelName]model.LabelValue)))
			}
			for k, v := range l {
				item.Metric[k] = v
			}
		}
	}

	return nil

}

// MergeValues merges values `a` and `b` with the given antiAffinityBuffer.
// When dynamic is true, each merged SampleStream infers its own anti-
// affinity from the inter-sample spacing of the longer series and uses
// antiAffinityBuffer only as a fallback when there isn't enough data to
// estimate. See #734.
func MergeValues(antiAffinityBuffer model.Time, dynamic bool, a, b model.Value, preferMax bool) (model.Value, error) {
	if a == nil {
		return b, nil
	}
	if b == nil {
		return a, nil
	}
	if a.Type() != b.Type() {
		return nil, fmt.Errorf("mismatch type %v!=%v", a.Type(), b.Type())
	}

	switch aTyped := a.(type) {
	// TODO: more logic? for now we assume both are correct if they exist
	// In the case where it is a single datapoint, we're going to assume that
	// either is valid, we just need one
	case *model.Scalar:
		bTyped := b.(*model.Scalar)

		if preferMax && bTyped.Value > aTyped.Value && bTyped.Timestamp != 0 {
			return bTyped, nil
		}

		if aTyped.Value != 0 && aTyped.Timestamp != 0 {
			return aTyped, nil
		}
		return bTyped, nil

	// In the case where it is a single datapoint, we're going to assume that
	// either is valid, we just need one
	case *model.String:
		bTyped := b.(*model.String)

		if aTyped.Value != "" && aTyped.Timestamp != 0 {
			return aTyped, nil
		}
		return bTyped, nil

	// List of *model.Sample -- only 1 value (guaranteed same timestamp)
	case model.Vector:
		bTyped := b.(model.Vector)

		newValue := make(model.Vector, 0, len(aTyped)+len(bTyped))
		fingerPrintMap := make(map[model.Fingerprint]int)

		addItem := func(item *model.Sample) {
			finger := item.Metric.Fingerprint()

			// If we've seen this fingerPrint before, lets make sure that a value exists
			if index, ok := fingerPrintMap[finger]; ok {
				existing := newValue[index]
				// If either side carries a histogram (Value/Histogram are
				// mutually exclusive on a Sample), keep the first non-empty
				// observation. preferMax only applies to float-vs-float.
				if existing.Histogram != nil || item.Histogram != nil {
					if existing.Histogram == nil && existing.Value == 0 && item.Histogram != nil {
						newValue[index] = item
					}
					return
				}
				// Only replace if we have no value (which seems reasonable)
				// Or we prefer max value and there is a bigger value
				if existing.Value == model.SampleValue(0) || preferMax && existing.Value < item.Value {
					newValue[index].Value = item.Value
				}
			} else {
				newValue = append(newValue, item)
				fingerPrintMap[finger] = len(newValue) - 1
			}
		}

		for _, item := range aTyped {
			addItem(item)
		}

		for _, item := range bTyped {
			addItem(item)
		}
		return newValue, nil

	case model.Matrix:
		bTyped := b.(model.Matrix)

		newValue := make(model.Matrix, 0, len(aTyped)+len(bTyped))
		fingerPrintMap := make(map[model.Fingerprint]int)

		addStream := func(stream *model.SampleStream) {
			finger := stream.Metric.Fingerprint()

			// If we've seen this fingerPrint before, lets make sure that a value exists
			if index, ok := fingerPrintMap[finger]; ok {
				// TODO: check this error? For now the only one is sig collision, which we check
				newValue[index], _ = MergeSampleStream(antiAffinityBuffer, dynamic, newValue[index], stream, preferMax)
			} else {
				newValue = append(newValue, stream)
				fingerPrintMap[finger] = len(newValue) - 1
			}
		}

		for _, item := range aTyped {
			addStream(item)
		}

		for _, item := range bTyped {
			addStream(item)
		}
		return newValue, nil
	}

	return nil, fmt.Errorf("unknown type! %v", reflect.TypeOf(a))
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
	at := make([]model.Time, len(a))
	for i, p := range a {
		at[i] = p.Timestamp
	}
	bt := make([]model.Time, len(b))
	for i, p := range b {
		bt[i] = p.Timestamp
	}
	return dynamicAntiAffinityFromTimes(at, bt)
}

// dynamicAntiAffinityFromTimes is the timestamp-only worker behind
// dynamicAntiAffinity. Lets the histogram path (which carries
// SampleHistogramPair, not SamplePair) share the same estimator.
func dynamicAntiAffinityFromTimes(a, b []model.Time) (model.Time, bool) {
	const minDynamicGaps = 3

	gaps := make([]model.Time, 0, len(a))
	for i := 1; i < len(a); i++ {
		if d := a[i] - a[i-1]; d > 0 {
			gaps = append(gaps, d)
		}
	}
	// Borrow gaps from b only when a is too short — keeps the estimate
	// rooted in the longer series rather than averaging across two
	// possibly-different scrape rates.
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

	ha := histogramTimestamps(a.Histograms)
	hb := histogramTimestamps(b.Histograms)
	if len(hb) > len(ha) {
		ha, hb = hb, ha
	}
	return dynamicAntiAffinityFromTimes(ha, hb)
}

func histogramTimestamps(s []model.SampleHistogramPair) []model.Time {
	ts := make([]model.Time, len(s))
	for i, p := range s {
		ts[i] = p.Timestamp
	}
	return ts
}
