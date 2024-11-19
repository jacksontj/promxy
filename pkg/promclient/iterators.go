package promclient

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
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
	return &SeriesIterator{V: v, offset: -1}
}

func toFpoints(promqlFloats []promql.FPoint) []FPoint {
	result := make([]FPoint, len(promqlFloats))
	for i, promqlFloat := range promqlFloats {
		result[i] = FPoint{promqlFloat.T, promqlFloat.F}
	}
	return result
}

// TODO: Expand on this
func toFloatHistogram(sampleHistogram *model.SampleHistogram) *histogram.FloatHistogram {
	return &histogram.FloatHistogram{
		CounterResetHint: 0,
		Schema:           0,
		ZeroThreshold:    0,
		ZeroCount:        0,
		Count:            float64(sampleHistogram.Count),
		Sum:              float64(sampleHistogram.Sum),
		PositiveSpans:    nil,
		NegativeSpans:    nil,
		PositiveBuckets:  nil,
		NegativeBuckets:  nil,
		CustomValues:     nil,
	}
}

// FPoint represents a single float data point for a given timestamp.
type FPoint struct {
	T int64
	F float64
}

// HPoint represents a single histogram data point for a given timestamp.
// H must never be nil.
type HPoint struct {
	T int64
	H *histogram.FloatHistogram
}

// SeriesIterator implements the prometheus SeriesIterator interface
type SeriesIterator struct {
	V      interface{}
	offset int
}

//type SeriesIterator struct {
//	floats               []FPoint
//	histograms           []HPoint
//	iFloats, iHistograms int
//	currT                int64
//	currF                float64
//	currH                *histogram.FloatHistogram
//}

// Seek advances the iterator forward to the value at or after
// the given timestamp.
func (s *SeriesIterator) Seek(t int64) chunkenc.ValueType {
	switch valueTyped := s.V.(type) {
	case *model.Scalar: // From a vector
		if int64(valueTyped.Timestamp) >= t {
			return chunkenc.ValFloat
		} else {
			return chunkenc.ValNone
		}
	case *model.Sample: // From a vector
		if int64(valueTyped.Timestamp) >= t {
			if valueTyped.Histogram != nil {
				return chunkenc.ValHistogram
			} else {
				return chunkenc.ValFloat
			}
		} else {
			return chunkenc.ValNone
		}
	case *model.SampleStream: // from a Matrix
		// If someone calls Seek() on an empty SampleStream, just return false
		if len(valueTyped.Values) != 0 {
			for i := s.offset; i < len(valueTyped.Values); i++ {
				s.offset = i
				if int64(valueTyped.Values[s.offset].Timestamp) >= t {
					return chunkenc.ValFloat
				}
			}
		} else if len(valueTyped.Histograms) != 0 {
			for i := s.offset; i < len(valueTyped.Histograms); i++ {
				s.offset = i
				if int64(valueTyped.Values[s.offset].Timestamp) >= t {
					return chunkenc.ValHistogram
				}
			}
		} else {
			return chunkenc.ValNone
		}
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
	return chunkenc.ValNone
}

// At returns the current timestamp/value pair.
func (s *SeriesIterator) At() (t int64, v float64) {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		return int64(valueTyped.Timestamp), float64(valueTyped.Value)
	case *model.Sample: // From a vector
		return int64(valueTyped.Timestamp), float64(valueTyped.Value)
	case *model.SampleStream: // from a Matrix
		// We assume the list of values is in order, so we'll iterate backwards
		return int64(valueTyped.Values[s.offset].Timestamp), float64(valueTyped.Values[s.offset].Value)
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
}

func (s *SeriesIterator) AtHistogram(hist *histogram.Histogram) (int64, *histogram.Histogram) {
	switch valueTyped := s.V.(type) {
	case *model.Sample:
		return int64(valueTyped.Timestamp), nil
	case *model.SampleStream: // from a Matrix
		// We assume the list of values is in order, so we'll iterate backwards
		return int64(valueTyped.Values[s.offset].Timestamp), nil
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
}

// TODO: Implement this for native histogram
func (s *SeriesIterator) AtFloatHistogram(hist *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	switch valueTyped := s.V.(type) {
	case *model.Sample:
		if valueTyped.Histogram != nil {
			return int64(valueTyped.Timestamp), toFloatHistogram(valueTyped.Histogram)
		}
		return int64(valueTyped.Timestamp), nil
	case *model.SampleStream: // from a Matrix
		return int64(valueTyped.Histograms[s.offset].Timestamp), toFloatHistogram(valueTyped.Histograms[s.offset].Histogram)
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}

}

func (s *SeriesIterator) AtT() int64 {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		return int64(valueTyped.Timestamp)
	case *model.Sample: // From a vector
		return int64(valueTyped.Timestamp)
	case *model.SampleStream: // from a Matrix
		// We assume the list of values is in order, so we'll iterate backwards
		return int64(valueTyped.Values[s.offset].Timestamp)
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
}

func (s *SeriesIterator) Next() chunkenc.ValueType {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		if s.offset < 0 {
			s.offset = 0
			return chunkenc.ValFloat
		} else {
			return chunkenc.ValNone
		}
	case *model.Sample: // From a vector
		if s.offset < 0 {
			s.offset = 0
			if valueTyped.Histogram != nil {
				return chunkenc.ValHistogram
			} else {
				return chunkenc.ValFloat
			}
		} else {
			return chunkenc.ValNone
		}
	case *model.SampleStream: // from a Matrix
		if s.offset < (len(valueTyped.Values) - 1) {
			s.offset++
			return chunkenc.ValFloat
		} else if s.offset < (len(valueTyped.Histograms) - 1) {
			s.offset++
			return chunkenc.ValHistogram
		} else {
			return chunkenc.ValNone
		}
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
	return chunkenc.ValNone
}

// Err returns the current error.
func (s *SeriesIterator) Err() error {
	return nil
}

// Labels returns the labels of the series that the iterator corresponds to.
func (s *SeriesIterator) Labels() labels.Labels {
	switch valueTyped := s.V.(type) {
	case *model.Scalar:
		panic("Unknown metric() scalar?")
	case *model.Sample: // From a vector
		ret := make(labels.Labels, 0, len(valueTyped.Metric))
		for k, v := range valueTyped.Metric {
			ret = append(ret, labels.Label{string(k), string(v)})
		}
		// TODO: move this into prom
		// there is no reason me (the series iterator) should have to sort these
		// if prom needs them sorted sometimes it should be responsible for doing so
		sort.Sort(ret)
		return ret
	case *model.SampleStream:
		ret := make(labels.Labels, 0, len(valueTyped.Metric))
		for k, v := range valueTyped.Metric {
			ret = append(ret, labels.Label{string(k), string(v)})
		}
		// TODO: move this into prom
		// there is no reason me (the series iterator) should have to sort these
		// if prom needs them sorted sometimes it should be responsible for doing so
		sort.Sort(ret)
		return ret
	default:
		panic("Unknown data type!")
	}
}
