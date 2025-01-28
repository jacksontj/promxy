package promclient

import (
	"fmt"
	"math"
	"reflect"
	"sort"

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
func NewSeriesIterator(v interface{}) *SeriesIterator {
	return &SeriesIterator{V: v, offset: -1}
}

// Zero buckets are on either side of 0
func zeroBucketInfo(buckets model.HistogramBuckets) (float64, float64) {
	for _, bucket := range buckets {
		if bucket.Boundaries == 3 && bucket.Lower < 0 && bucket.Upper > 0 {
			return float64(bucket.Upper), float64(bucket.Count)
		}
	}
	return 0.0, 0.0
}

func separateBuckets(buckets model.HistogramBuckets) ([]model.HistogramBucket, []model.HistogramBucket) {
	negativeBuckets := make([]model.HistogramBucket, 0)
	positiveBuckets := make([]model.HistogramBucket, 0)
	// Negative buckets need to be reversed
	for _, bucket := range buckets {
		if bucket.Lower < 0 && bucket.Upper < 0 {
			negativeBuckets = append([]model.HistogramBucket{*bucket}, negativeBuckets...)
		} else if bucket.Lower > 0 && bucket.Upper > 0 {
			positiveBuckets = append(positiveBuckets, *bucket)
		}
	}
	return negativeBuckets, positiveBuckets
}

func fetchBucketCount(negativePromBuckets []model.HistogramBucket, positivePromBuckets []model.HistogramBucket) ([]float64, []float64) {
	negativeNativeBuckets := make([]float64, 0)
	positiveNativeBuckets := make([]float64, 0)
	for _, bucket := range negativePromBuckets {
		negativeNativeBuckets = append(negativeNativeBuckets, float64(bucket.Count))
	}
	for _, bucket := range positivePromBuckets {
		positiveNativeBuckets = append(positiveNativeBuckets, float64(bucket.Count))
	}
	return negativeNativeBuckets, positiveNativeBuckets
}

// Get schema from bucket
func deriveSchema(buckets model.HistogramBuckets) (int32, float64) {
	for _, bucket := range buckets {
		if bucket.Lower > 0 && bucket.Upper > 0 {
			jump := float64(bucket.Upper) / float64(bucket.Lower)
			schema := int32(math.Round(math.Log2(1 / math.Log2(jump))))
			return schema, jump
		} else if bucket.Lower < 0 && bucket.Upper < 0 {
			jump := float64(bucket.Lower) / float64(bucket.Upper)
			schema := int32(math.Round(math.Log2(1 / math.Log2(float64(bucket.Lower)/float64(bucket.Upper)))))
			return schema, jump
		}
	}
	return 0, 2.0
}

// Bucket with index = 0 boundary starts with 1 or -1 in case of negative spans
func fetchSpans(negativePromBuckets []model.HistogramBucket, positivePromBuckets []model.HistogramBucket, jump float64) ([]histogram.Span, []histogram.Span) {
	positiveSpans := make([]histogram.Span, 0)
	negativeSpans := make([]histogram.Span, 0)
	tolerance := math.Ceil(jump / 2.0)
	//deduce positive spans
	if len(positivePromBuckets) > 0 {
		maxBucketUpperLimit := float64(positivePromBuckets[len(positivePromBuckets)-1].Upper)
		minBucketUpperLimit := float64(positivePromBuckets[0].Upper)
		currOffset := 0
		upper := 1.0
		if minBucketUpperLimit < 1.0 {
			initialValue := 1.0
			for initialValue > minBucketUpperLimit {
				currOffset--
				initialValue = initialValue / jump
			}
			upper = minBucketUpperLimit
		}
		// At this point we have evaluated currOffset to be 0 or -ve depending on what's the start of bucket
		bucketIndex := 0
		currLength := 0
		assigned := true
		for upper <= maxBucketUpperLimit+tolerance && bucketIndex <= len(positivePromBuckets)-1 {
			if math.Abs(float64(positivePromBuckets[bucketIndex].Upper)-upper) < tolerance {
				assigned = false
				currLength++
				bucketIndex++
			} else {
				if !assigned {
					positiveSpans = append(positiveSpans, histogram.Span{int32(currOffset), uint32(currLength)})
					currLength = 0
					currOffset = 0
					assigned = true
				}
				currOffset++
			}
			upper = upper * jump
		}
		if currLength > 0 {
			positiveSpans = append(positiveSpans, histogram.Span{int32(currOffset), uint32(currLength)})
		}
	}
	//deduce negative spans
	if len(negativePromBuckets) > 0 {
		minBucketLowerLimit := float64(negativePromBuckets[len(negativePromBuckets)-1].Lower)
		maxBucketUpperLimit := float64(negativePromBuckets[0].Lower)
		currOffset := 0
		lower := -1.0
		if maxBucketUpperLimit > -1.0 {
			initialValue := -1.0
			for initialValue < maxBucketUpperLimit {
				currOffset--
				initialValue = initialValue / jump // Increasing the offset it by dividing it by jump
			}
			lower = maxBucketUpperLimit
		}
		bucketIndex := 0
		currLength := 0
		assigned := true
		for lower > minBucketLowerLimit-tolerance && bucketIndex <= len(negativePromBuckets)-1 {
			if math.Abs(float64(negativePromBuckets[bucketIndex].Lower)-lower) < tolerance {
				assigned = false
				currLength++
				bucketIndex++
			} else {
				if !assigned {
					negativeSpans = append(negativeSpans, histogram.Span{int32(currOffset), uint32(currLength)})
					currLength = 0
					currOffset = 0
					assigned = true
				}
				currOffset++
			}
			lower = lower * jump
		}
		if currLength > 0 {
			negativeSpans = append(negativeSpans, histogram.Span{int32(currOffset), uint32(currLength)})
		}
	}

	return negativeSpans, positiveSpans
}

func toFloatHistogram(sampleHistogram *model.SampleHistogram) *histogram.FloatHistogram {
	zeroBucketThreshold, zeroBucketCount := zeroBucketInfo(sampleHistogram.Buckets)
	derivedSchema, derivedBucketJump := deriveSchema(sampleHistogram.Buckets)
	negativePromBuckets, positivePromBuckets := separateBuckets(sampleHistogram.Buckets)
	negativeBuckets, positiveBuckets := fetchBucketCount(negativePromBuckets, positivePromBuckets)
	negativeSpans, positiveSpans := fetchSpans(negativePromBuckets, positivePromBuckets, derivedBucketJump)
	return &histogram.FloatHistogram{
		ZeroCount:       zeroBucketCount,
		ZeroThreshold:   zeroBucketThreshold,
		Count:           float64(sampleHistogram.Count),
		Sum:             float64(sampleHistogram.Sum),
		Schema:          derivedSchema,
		PositiveSpans:   positiveSpans,
		PositiveBuckets: positiveBuckets,
		NegativeSpans:   negativeSpans,
		NegativeBuckets: negativeBuckets,
	}
}

// SeriesIterator implements the prometheus SeriesIterator interface
type SeriesIterator struct {
	V      interface{}
	offset int
}

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
				if int64(valueTyped.Histograms[s.offset].Timestamp) >= t {
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
		if len(valueTyped.Values) != 0 {
			return int64(valueTyped.Values[s.offset].Timestamp)
		} else if len(valueTyped.Histograms) != 0 {
			return int64(valueTyped.Histograms[s.offset].Timestamp)
		} else {
			msg := fmt.Sprintf("Unknown data type %v", reflect.TypeOf(s.V))
			panic(msg)
		}
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
