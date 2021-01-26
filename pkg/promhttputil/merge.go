package promhttputil

import (
	"errors"
	"fmt"
	"reflect"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage"
)

// WarningsConvert simply converts v1.Warnings to storage.Warnings
func WarningsConvert(ws v1.Warnings) storage.Warnings {
	w := make(storage.Warnings, len(ws))
	for i, item := range ws {
		w[i] = errors.New(item)
	}
	return w
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

// MergeValues merges values `a` and `b` with the given antiAffinityBuffer
// TODO: always make copies? Now we sometimes return one, or make a copy, or do nothing
func MergeValues(antiAffinityBuffer model.Time, a, b model.Value) (model.Value, error) {
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
				// TODO: better? For now we only replace if we have no value (which seems reasonable)
				if newValue[index].Value == model.SampleValue(0) {
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
				newValue[index], _ = MergeSampleStream(antiAffinityBuffer, newValue[index], stream)
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

// MergeSampleStream merges SampleStreams `a` and `b` with the given antiAffinityBuffer
// When combining series from 2 different prometheus hosts we can run into some problems
// with clock skew (from a variety of sources). The primary one I've run into is issues
// with the time that prometheus stores. Since the time associated with the datapoint is
// the *start* time of the scrape, there can be quite a lot of time (which can vary
// dramatically between hosts) for the exporter to return. In an attempt to mitigate
// this problem we're going to *not* merge any datapoint within antiAffinityBuffer of another point
// we have. This means we can tolerate antiAffinityBuffer/2 on either side (which can be used by either
// clock skew or from this scrape skew).
func MergeSampleStream(antiAffinityBuffer model.Time, a, b *model.SampleStream) (*model.SampleStream, error) {
	if a.Metric.Fingerprint() != b.Metric.Fingerprint() {
		return nil, fmt.Errorf("cannot merge mismatch fingerprints")
	}

	// if either set of values are empty, return the one with data
	if len(a.Values) == 0 {
		return b, nil
	} else if len(b.Values) == 0 {
		return a, nil
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

	// TODO: really there should be a library method for this in prometheus IMO
	// At this point we have 2 sorted lists of datapoints which we need to merge
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
		newValues = append(newValues, aValue)
	}

	lastTime := newValues[len(newValues)-1].Timestamp
	for ; bOffset < len(b.Values); bOffset++ {
		bValue := b.Values[bOffset]
		if bValue.Timestamp > lastTime+antiAffinityBuffer {
			newValues = append(newValues, bValue)
		}
	}

	return &model.SampleStream{
		Metric: a.Metric,
		Values: newValues,
	}, nil
}
