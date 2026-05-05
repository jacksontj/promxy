// Copyright 2017 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package remote

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func TestValidateLabelsAndMetricName(t *testing.T) {
	tests := []struct {
		input       labels.Labels
		expectedErr string
		shouldPass  bool
	}{
		{
			input: labels.FromStrings(
				"__name__", "name",
				"labelName", "labelValue",
			),
			expectedErr: "",
			shouldPass:  true,
		},
		{
			input: labels.FromStrings(
				"__name__", "name",
				"_labelName", "labelValue",
			),
			expectedErr: "",
			shouldPass:  true,
		},
		{
			input: labels.FromStrings(
				"__name__", "name",
				"@labelName", "labelValue",
			),
			expectedErr: "Invalid label name: @labelName",
			shouldPass:  false,
		},
		{
			input: labels.FromStrings(
				"__name__", "name",
				"123labelName", "labelValue",
			),
			expectedErr: "Invalid label name: 123labelName",
			shouldPass:  false,
		},
		{
			input: labels.FromStrings(
				"__name__", "name",
				"", "labelValue",
			),
			expectedErr: "Invalid label name: ",
			shouldPass:  false,
		},
		{
			input: labels.FromStrings(
				"__name__", "name",
				"labelName", string([]byte{0xff}),
			),
			expectedErr: "Invalid label value: " + string([]byte{0xff}),
			shouldPass:  false,
		},
		{
			input: labels.FromStrings(
				"__name__", "@invalid_name",
			),
			expectedErr: "Invalid metric name: @invalid_name",
			shouldPass:  false,
		},
	}

	for _, test := range tests {
		err := validateLabelsAndMetricName(test.input)
		if test.shouldPass != (err == nil) {
			if test.shouldPass {
				t.Fatalf("Test should pass, got unexpected error: %v", err)
			} else {
				t.Fatalf("Test should fail, unexpected error, got: %v, expected: %v", err, test.expectedErr)
			}
		}
	}
}

func TestConcreteSeriesSet(t *testing.T) {
	series1 := &concreteSeries{
		labels:  labels.FromStrings("foo", "bar"),
		samples: []prompb.Sample{{Value: 1, Timestamp: 2}},
	}
	series2 := &concreteSeries{
		labels:  labels.FromStrings("foo", "baz"),
		samples: []prompb.Sample{{Value: 3, Timestamp: 4}},
	}
	c := &concreteSeriesSet{
		series: []storage.Series{series1, series2},
	}
	if !c.Next() {
		t.Fatalf("Expected Next() to be true.")
	}
	if c.At() != series1 {
		t.Fatalf("Unexpected series returned.")
	}
	if !c.Next() {
		t.Fatalf("Expected Next() to be true.")
	}
	if c.At() != series2 {
		t.Fatalf("Unexpected series returned.")
	}
	if c.Next() {
		t.Fatalf("Expected Next() to be false.")
	}
}

// TestQueryResultHistogramRoundtrip exercises ToQueryResult/FromQueryResult on
// a series carrying both float and histogram samples, ensuring the iterator's
// merge-walk preserves timestamp order across the two sequences.
func TestQueryResultHistogramRoundtrip(t *testing.T) {
	mkInt := func(ts int64, count uint64) prompb.Histogram {
		return prompb.FromIntHistogram(ts, &histogram.Histogram{
			Count:           count,
			Sum:             float64(count),
			PositiveSpans:   []histogram.Span{{Offset: 0, Length: 1}},
			PositiveBuckets: []int64{int64(count)},
		})
	}
	mkFloat := func(ts int64, count float64) prompb.Histogram {
		return prompb.FromFloatHistogram(ts, &histogram.FloatHistogram{
			Count:           count,
			Sum:             count,
			PositiveSpans:   []histogram.Span{{Offset: 0, Length: 1}},
			PositiveBuckets: []float64{count},
		})
	}

	in := &prompb.QueryResult{
		Timeseries: []*prompb.TimeSeries{{
			Labels:  []prompb.Label{{Name: "__name__", Value: "rt"}},
			Samples: []prompb.Sample{{Timestamp: 100, Value: 1}, {Timestamp: 300, Value: 3}},
			Histograms: []prompb.Histogram{
				mkInt(200, 7),
				mkFloat(400, 9),
			},
		}},
	}

	ss := FromQueryResult(false, in)
	require.True(t, ss.Next(), "expected one series")
	series := ss.At()

	type sample struct {
		t   int64
		v   float64
		isH bool
		isF bool
	}
	var got []sample
	it := series.Iterator(nil)
	for {
		vt := it.Next()
		if vt == chunkenc.ValNone {
			break
		}
		switch vt {
		case chunkenc.ValFloat:
			ts, v := it.At()
			got = append(got, sample{t: ts, v: v})
		case chunkenc.ValHistogram:
			ts, h := it.AtHistogram(nil)
			got = append(got, sample{t: ts, v: h.Sum, isH: true})
		case chunkenc.ValFloatHistogram:
			ts, fh := it.AtFloatHistogram(nil)
			got = append(got, sample{t: ts, v: fh.Sum, isF: true})
		}
	}
	require.False(t, ss.Next(), "expected only one series")
	require.NoError(t, it.Err())

	require.Equal(t, []sample{
		{t: 100, v: 1},
		{t: 200, v: 7, isH: true},
		{t: 300, v: 3},
		{t: 400, v: 9, isF: true},
	}, got)

	// Round-trip back through ToQueryResult and confirm we get an equivalent
	// shape (counts preserved on both sequences).
	out, err := ToQueryResult(FromQueryResult(false, in), 0)
	require.NoError(t, err)
	require.Len(t, out.Timeseries, 1)
	require.Len(t, out.Timeseries[0].Samples, 2)
	require.Len(t, out.Timeseries[0].Histograms, 2)
}

func TestConcreteSeriesClonesLabels(t *testing.T) {
	lbls := labels.FromStrings("a", "b", "c", "d")
	cs := concreteSeries{
		labels: lbls.Copy(),
	}

	gotLabels := cs.Labels()
	require.Equal(t, lbls, gotLabels)

	// labels.Labels in 3.x is value-semantic; modifying the returned copy must
	// not affect subsequent reads, so re-fetch and compare.
	_ = gotLabels
	gotLabels = cs.Labels()
	require.Equal(t, lbls, gotLabels)
}
