package promclient

import (
	"fmt"
	"reflect"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// IteratorsForValue returns SeriesIterators for the value passed in
func IteratorsForValue(v model.Value) []*SeriesIterator {
	switch valueTyped := v.(type) {
	case *model.Scalar:
		return []*SeriesIterator{NewSeriesIterator(v)}
	case *model.String:
		panic("Not implemented")
	case model.Vector:
		iterators := make([]*SeriesIterator, len(valueTyped))
		for i, sample := range valueTyped {
			iterators[i] = NewSeriesIterator(sample)
		}
		return iterators
	case model.Matrix:
		iterators := make([]*SeriesIterator, len(valueTyped))
		for i, stream := range valueTyped {
			iterators[i] = NewSeriesIterator(stream)
		}
		return iterators
	case nil:
		return nil
	default:
		msg := fmt.Sprintf("Unknown type %v", reflect.TypeOf(v))
		panic(msg)
	}
}

// NewSeriesIterator return a series iterator for the given value
// TODO: error return if the type is incorrect?
func NewSeriesIterator(v interface{}) *SeriesIterator {
	return &SeriesIterator{V: v, offset: -1, histOffset: -1}
}

// SeriesIterator implements the prometheus SeriesIterator interface.
//
// Float and histogram samples can coexist on a single SampleStream, so the
// iterator walks both sequences in timestamp order. The "last" field tracks
// which sequence supplied the current sample so the At* methods know which
// slice to read.
type SeriesIterator struct {
	V          interface{}
	offset     int
	histOffset int
	last       chunkenc.ValueType
}

// peekFloat returns (timestamp, ok) for the float sample at the next position
// without advancing.
func (s *SeriesIterator) peekFloat() (int64, bool) {
	switch v := s.V.(type) {
	case *model.Scalar:
		if s.offset < 0 {
			return int64(v.Timestamp), true
		}
	case *model.Sample:
		if v.Histogram != nil {
			return 0, false
		}
		if s.offset < 0 {
			return int64(v.Timestamp), true
		}
	case *model.SampleStream:
		if s.offset+1 < len(v.Values) {
			return int64(v.Values[s.offset+1].Timestamp), true
		}
	}
	return 0, false
}

// peekHist returns (timestamp, *FloatHistogram, ok) for the histogram sample at
// the next position without advancing. The returned histogram is the result of
// converting model.SampleHistogram → histogram.FloatHistogram; while that
// converter returns nil (HTTP-API pass-through limitation), this effectively
// reports no histogram samples to the engine.
func (s *SeriesIterator) peekHist() (int64, *histogram.FloatHistogram, bool) {
	switch v := s.V.(type) {
	case *model.Sample:
		if v.Histogram == nil {
			return 0, nil, false
		}
		if s.histOffset < 0 {
			fh := sampleHistogramToFloatHistogram(v.Histogram)
			if fh == nil {
				return 0, nil, false
			}
			return int64(v.Timestamp), fh, true
		}
	case *model.SampleStream:
		if s.histOffset+1 < len(v.Histograms) {
			p := v.Histograms[s.histOffset+1]
			fh := sampleHistogramToFloatHistogram(p.Histogram)
			if fh == nil {
				return 0, nil, false
			}
			return int64(p.Timestamp), fh, true
		}
	}
	return 0, nil, false
}

// Seek advances the iterator forward to the value at or after the given
// timestamp. If the iterator is already positioned at or after t, it does
// not advance.
func (s *SeriesIterator) Seek(t int64) chunkenc.ValueType {
	if s.last != chunkenc.ValNone && s.AtT() >= t {
		return s.last
	}
	for {
		vt := s.Next()
		if vt == chunkenc.ValNone {
			return chunkenc.ValNone
		}
		if s.AtT() >= t {
			return vt
		}
	}
}

// At returns the current timestamp/value pair.
func (s *SeriesIterator) At() (t int64, v float64) {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		return int64(valueTyped.Timestamp), float64(valueTyped.Value)
	case *model.Sample:
		return int64(valueTyped.Timestamp), float64(valueTyped.Value)
	case *model.SampleStream:
		return int64(valueTyped.Values[s.offset].Timestamp), float64(valueTyped.Values[s.offset].Value)
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
}

// Next advances the iterator by one, picking the float or histogram sample
// with the smaller next-timestamp. Histogram samples that fail conversion to
// histogram.FloatHistogram are skipped.
func (s *SeriesIterator) Next() chunkenc.ValueType {
	for {
		floatT, hasFloat := s.peekFloat()
		histT, _, hasHist := s.peekHist()

		switch {
		case !hasFloat && !hasHist:
			s.last = chunkenc.ValNone
			return chunkenc.ValNone
		case hasFloat && (!hasHist || floatT <= histT):
			s.offset++
			s.last = chunkenc.ValFloat
			return chunkenc.ValFloat
		default:
			s.histOffset++
			s.last = chunkenc.ValFloatHistogram
			return chunkenc.ValFloatHistogram
		}
	}
}

// AtHistogram returns (0, nil) — promxy never produces integer histograms; the
// engine accepts FloatHistogram for evaluation, so AtFloatHistogram is the
// only path used.
func (s *SeriesIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	return 0, nil
}

// AtFloatHistogram returns the current histogram sample. Only valid after
// Next/Seek returned ValFloatHistogram. The hint follows the chunkenc.Iterator
// contract: when non-nil it must receive a deep copy so the caller can keep
// the result across subsequent Next() calls without aliasing the iterator's
// internal state. The PromQL engine relies on this — see promql/engine.go
// "Make sure to pass non-nil H to AtFloatHistogram so that it does a
// deep-copy".
func (s *SeriesIterator) AtFloatHistogram(hint *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	var t int64
	var src *histogram.FloatHistogram
	switch v := s.V.(type) {
	case *model.Sample:
		t = int64(v.Timestamp)
		src = sampleHistogramToFloatHistogram(v.Histogram)
	case *model.SampleStream:
		p := v.Histograms[s.histOffset]
		t = int64(p.Timestamp)
		src = sampleHistogramToFloatHistogram(p.Histogram)
	default:
		return 0, nil
	}
	if src == nil {
		return t, nil
	}
	if hint == nil {
		return t, src
	}
	src.CopyTo(hint)
	return t, hint
}

// AtT implements chunkenc.Iterator.
func (s *SeriesIterator) AtT() int64 {
	switch v := s.V.(type) {
	case *model.Scalar:
		return int64(v.Timestamp)
	case *model.Sample:
		return int64(v.Timestamp)
	case *model.SampleStream:
		if s.last == chunkenc.ValFloatHistogram {
			return int64(v.Histograms[s.histOffset].Timestamp)
		}
		return int64(v.Values[s.offset].Timestamp)
	}
	return 0
}

// Err returns the current error.
func (s *SeriesIterator) Err() error {
	return nil
}

// Labels returns the labels of the series that the iterator corresponds to.
func (s *SeriesIterator) Labels() labels.Labels {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		return labels.EmptyLabels() // A scalar has (by definition) no labels
	case *model.Sample: // From a vector
		return metricToLabels(valueTyped.Metric)
	case *model.SampleStream:
		return metricToLabels(valueTyped.Metric)
	default:
		panic("Unknown data type!")
	}
}

func metricToLabels(m model.Metric) labels.Labels {
	b := labels.NewScratchBuilder(len(m))
	for k, v := range m {
		b.Add(string(k), string(v))
	}
	b.Sort()
	return b.Labels()
}
