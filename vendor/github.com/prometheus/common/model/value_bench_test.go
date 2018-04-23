package model

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/mailru/easyjson"
)

func generateData(timeseries, datapoints int) Matrix {
	// Create the top-level matrix
	m := make(Matrix, 0)

	for i := 0; i < timeseries; i++ {
		lset := map[LabelName]LabelValue{
			MetricNameLabel: LabelValue("timeseries_" + strconv.Itoa(i)),
		}

		now := Now()

		values := make([]SamplePair, datapoints)

		for x := datapoints; x > 0; x-- {
			values[x-1] = SamplePair{
				Timestamp: now.Add(time.Second * -15 * time.Duration(x)), // Set the time back assuming a 15s interval
				Value:     SampleValue(float64(x)),
			}
		}

		ss := &SampleStream{
			Metric: Metric(lset),
			Values: values,
		}

		m = append(m, ss)
	}
	return m
}

var benchBytes []byte

func doMarshal(b *testing.B, v interface{}) {
	b.Run("encoding/json", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				benchBytes, _ = json.Marshal(v)
			}
		})
		b.Run("unmarshal", func(b *testing.B) {
			for n := 0; n < b.N; n++ {
				if err := json.Unmarshal(benchBytes, &v); err != nil {
					b.Fatalf("err: %v", err)
				}
			}
		})
	})

	b.Run("easyjson", func(b *testing.B) {
		b.Run("marshal", func(b *testing.B) {
			m, ok := v.(easyjson.Marshaler)
			if !ok {
				t := reflect.TypeOf(v)
				fmt.Println(t, "not easyjson.Marshaler")
				b.Skip()
			}

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				easyjson.MarshalToWriter(m, ioutil.Discard)
			}
		})
		b.Run("unmarshal", func(b *testing.B) {
			m, ok := v.(easyjson.Unmarshaler)
			if !ok {
				t := reflect.TypeOf(v)
				fmt.Println(t, "not easyjson.Unmarshaler")
				b.Skip()
			}

			b.ResetTimer()

			for n := 0; n < b.N; n++ {
				if err := easyjson.Unmarshal(benchBytes, m); err != nil {
					b.Fatalf("err: %v", err)
				}
			}
		})
	})
}

func BenchmarkJSON(b *testing.B) {
	NUM_TIMESERIES := 500
	NUM_DATAPOINTS := 100
	m := generateData(NUM_TIMESERIES, NUM_DATAPOINTS)

	// Single float64
	b.Run("SampleValue", func(b *testing.B) {
		doMarshal(b, m[0].Values[0].Value)
	})

	// Single timestamp
	b.Run("Timestamp", func(b *testing.B) {
		doMarshal(b, m[0].Values[0].Timestamp)
	})

	// Single pair of value and timestamp
	b.Run("SamplePair", func(b *testing.B) {
		doMarshal(b, m[0].Values[0])
	})

	// Labelset for the metrics
	b.Run("labelset", func(b *testing.B) {
		doMarshal(b, m[0].Metric)
	})

	// labelset + []SamplePair
	b.Run("SampleStream", func(b *testing.B) {
		doMarshal(b, m[0])
	})

	// labelset + []SamplePair
	b.Run("Matrix", func(b *testing.B) {
		doMarshal(b, m)
	})
}
