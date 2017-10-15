package promclient

import (
	"fmt"
	"reflect"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/metric"
)

func IteratorsForValue(v model.Value) []*SeriesIterator {
	switch valueTyped := v.(type) {
	case *model.Scalar:
		return []*SeriesIterator{&SeriesIterator{v}}
	case *model.String:
		panic("Not implemented")
	case model.Vector:
		iterators := make([]*SeriesIterator, len(valueTyped))
		for i, sample := range valueTyped {
			iterators[i] = &SeriesIterator{sample}
		}
		return iterators
	case model.Matrix:
		iterators := make([]*SeriesIterator, len(valueTyped))
		for i, stream := range valueTyped {
			iterators[i] = &SeriesIterator{stream}
		}
		return iterators
	case nil:
		return nil
	default:
		msg := fmt.Sprintf("Unknown type %v", reflect.TypeOf(v))
		panic(msg)
		return nil
	}
}

// Iterators for client return data

func NewSeriesIterator(v model.Value) *SeriesIterator {
	return &SeriesIterator{v}
}

type SeriesIterator struct{ v interface{} }

// Gets the value that is closest before the given time. In case a value
// exists at precisely the given time, that value is returned. If no
// applicable value exists, model.ZeroSamplePair is returned.
func (s *SeriesIterator) ValueAtOrBeforeTime(t model.Time) model.SamplePair {
	switch valueTyped := s.v.(type) {
	case *model.Sample: // From a vector
		if int64(valueTyped.Timestamp) <= int64(t) {
			return model.SamplePair{
				valueTyped.Timestamp,
				valueTyped.Value,
			}
		} else {
			return model.ZeroSamplePair
		}
	case *model.SampleStream: // from a Matrix
		// We assume the list of values is in order, so we'll iterate backwards
		for i := len(valueTyped.Values) - 1; i >= 0; i-- {
			if int64(valueTyped.Values[i].Timestamp) <= int64(t) {
				return valueTyped.Values[i]
			}
		}
		return model.ZeroSamplePair
	default:
		panic("Unknown data type!")
	}

	return model.ZeroSamplePair
}

// TODO: test? not sure how to hit this
// Gets all values contained within a given interval.
func (s *SeriesIterator) RangeValues(interval metric.Interval) []model.SamplePair {
	switch valueTyped := s.v.(type) {
	case *model.Sample: // From a vector
		if int64(valueTyped.Timestamp) >= int64(interval.OldestInclusive) && int64(valueTyped.Timestamp) <= int64(interval.NewestInclusive) {
			return []model.SamplePair{
				model.SamplePair{
					valueTyped.Timestamp,
					valueTyped.Value,
				},
			}
		} else {
			return nil
		}
	case *model.SampleStream: // from a Matrix
		pairs := make([]model.SamplePair, 0, len(valueTyped.Values))
		for _, value := range valueTyped.Values {
			if int64(value.Timestamp) >= int64(interval.OldestInclusive) && int64(value.Timestamp) <= int64(interval.NewestInclusive) {
				pairs = append(pairs, value)
			}
		}
		if len(pairs) > 0 {
			return pairs
		} else {
			return nil
		}

	default:
		panic("Unknown data type!")
	}
}

// Returns the metric of the series that the iterator corresponds to.
func (s *SeriesIterator) Metric() metric.Metric {
	switch valueTyped := s.v.(type) {
	case *model.Scalar:
		panic("Unknown metric() scalar?")
	case *model.Sample: // From a vector
		return metric.Metric{
			Copied: true,
			Metric: valueTyped.Metric,
		}
	case *model.SampleStream:
		return metric.Metric{
			Copied: true,
			Metric: valueTyped.Metric,
		}
	default:
		panic("Unknown data type!")
	}
}

// Closes the iterator and releases the underlying data.
func (s *SeriesIterator) Close() {}
