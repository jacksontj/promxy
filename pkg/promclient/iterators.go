package promclient

import (
	"fmt"
	"reflect"
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
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

// SeriesIterator implements the prometheus SeriesIterator interface
type SeriesIterator struct {
	V      interface{}
	offset int
}

// Seek advances the iterator forward to the value at or after
// the given timestamp.
func (s *SeriesIterator) Seek(t int64) bool {
	switch valueTyped := s.V.(type) {
	case *model.Sample: // From a vector
		return int64(valueTyped.Timestamp) >= t
	case *model.SampleStream: // from a Matrix
		// If someone calls Seek() on an empty SampleStream, just return false
		if len(valueTyped.Values) == 0 {
			return false
		}
		for i := s.offset; i < len(valueTyped.Values); i++ {
			s.offset = i
			if int64(valueTyped.Values[s.offset].Timestamp) >= t {
				return true
			}
		}
		return false
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
}

// At returns the current timestamp/value pair.
func (s *SeriesIterator) At() (t int64, v float64) {
	switch valueTyped := s.V.(type) {
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

// Next advances the iterator by one.
func (s *SeriesIterator) Next() bool {
	switch valueTyped := s.V.(type) {
	case *model.Sample: // From a vector
		if s.offset < 0 {
			s.offset = 0
			return true
		}
		return false
	case *model.SampleStream: // from a Matrix
		if s.offset < (len(valueTyped.Values) - 1) {
			s.offset++
			return true
		}
		return false
	default:
		msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
		panic(msg)
	}
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
