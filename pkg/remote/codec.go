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
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"

	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/util/annotations"
)

// decodeReadLimit is the maximum size of a read request body in bytes.
const decodeReadLimit = 32 * 1024 * 1024

type HTTPError struct {
	msg    string
	status int
}

func (e HTTPError) Error() string {
	return e.msg
}

func (e HTTPError) Status() int {
	return e.status
}

// DecodeReadRequest reads a remote.Request from a http.Request.
func DecodeReadRequest(r *http.Request) (*prompb.ReadRequest, error) {
	compressed, err := io.ReadAll(io.LimitReader(r.Body, decodeReadLimit))
	if err != nil {
		return nil, err
	}

	reqBuf, err := snappy.Decode(nil, compressed)
	if err != nil {
		return nil, err
	}

	var req prompb.ReadRequest
	if err := proto.Unmarshal(reqBuf, &req); err != nil {
		return nil, err
	}

	return &req, nil
}

// EncodeReadResponse writes a remote.Response to a http.ResponseWriter.
func EncodeReadResponse(resp *prompb.ReadResponse, w http.ResponseWriter) error {
	data, err := proto.Marshal(resp)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "application/x-protobuf")
	w.Header().Set("Content-Encoding", "snappy")

	compressed := snappy.Encode(nil, data)
	_, err = w.Write(compressed)
	return err
}

// ToWriteRequest converts an array of samples into a WriteRequest proto.
func ToWriteRequest(samples []*model.Sample) *prompb.WriteRequest {
	req := &prompb.WriteRequest{
		Timeseries: make([]prompb.TimeSeries, 0, len(samples)),
	}

	for _, s := range samples {
		ts := prompb.TimeSeries{
			Labels: MetricToLabelProtos(s.Metric),
			Samples: []prompb.Sample{
				{
					Value:     float64(s.Value),
					Timestamp: int64(s.Timestamp),
				},
			},
		}
		req.Timeseries = append(req.Timeseries, ts)
	}

	return req
}

// ToQuery builds a Query proto.
func ToQuery(from, to int64, matchers []*labels.Matcher, p *storage.SelectHints) (*prompb.Query, error) {
	ms, err := toLabelMatchers(matchers)
	if err != nil {
		return nil, err
	}

	var rp *prompb.ReadHints
	if p != nil {
		rp = &prompb.ReadHints{
			StepMs:  p.Step,
			Func:    p.Func,
			StartMs: p.Start,
			EndMs:   p.End,
		}
	}

	return &prompb.Query{
		StartTimestampMs: from,
		EndTimestampMs:   to,
		Matchers:         ms,
		Hints:            rp,
	}, nil
}

// FromQuery unpacks a Query proto.
func FromQuery(req *prompb.Query) (int64, int64, []*labels.Matcher, *storage.SelectHints, error) {
	matchers, err := fromLabelMatchers(req.Matchers)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	var SelectHints *storage.SelectHints
	if req.Hints != nil {
		SelectHints = &storage.SelectHints{
			Start: req.Hints.StartMs,
			End:   req.Hints.EndMs,
			Step:  req.Hints.StepMs,
			Func:  req.Hints.Func,
		}
	}

	return req.StartTimestampMs, req.EndTimestampMs, matchers, SelectHints, nil
}

// ToQueryResult builds a QueryResult proto.
func ToQueryResult(ss storage.SeriesSet, sampleLimit int) (*prompb.QueryResult, error) {
	numSamples := 0
	resp := &prompb.QueryResult{}
	for ss.Next() {
		series := ss.At()
		iter := series.Iterator(nil)
		samples := []prompb.Sample{}
		var histograms []prompb.Histogram

	loop:
		for {
			vt := iter.Next()
			switch vt {
			case chunkenc.ValNone:
				break loop
			case chunkenc.ValFloat:
				numSamples++
				if sampleLimit > 0 && numSamples > sampleLimit {
					return nil, HTTPError{
						msg:    fmt.Sprintf("exceeded sample limit (%d)", sampleLimit),
						status: http.StatusBadRequest,
					}
				}
				ts, val := iter.At()
				samples = append(samples, prompb.Sample{
					Timestamp: ts,
					Value:     val,
				})
			case chunkenc.ValHistogram:
				numSamples++
				if sampleLimit > 0 && numSamples > sampleLimit {
					return nil, HTTPError{
						msg:    fmt.Sprintf("exceeded sample limit (%d)", sampleLimit),
						status: http.StatusBadRequest,
					}
				}
				ts, h := iter.AtHistogram(nil)
				histograms = append(histograms, prompb.FromIntHistogram(ts, h))
			case chunkenc.ValFloatHistogram:
				numSamples++
				if sampleLimit > 0 && numSamples > sampleLimit {
					return nil, HTTPError{
						msg:    fmt.Sprintf("exceeded sample limit (%d)", sampleLimit),
						status: http.StatusBadRequest,
					}
				}
				ts, fh := iter.AtFloatHistogram(nil)
				histograms = append(histograms, prompb.FromFloatHistogram(ts, fh))
			}
		}
		if err := iter.Err(); err != nil {
			return nil, err
		}

		resp.Timeseries = append(resp.Timeseries, &prompb.TimeSeries{
			Labels:     labelsToLabelsProto(series.Labels()),
			Samples:    samples,
			Histograms: histograms,
		})
	}
	if err := ss.Err(); err != nil {
		return nil, err
	}
	return resp, nil
}

// FromQueryResult unpacks a QueryResult proto.
func FromQueryResult(sortSeries bool, res *prompb.QueryResult) storage.SeriesSet {
	series := make([]storage.Series, 0, len(res.Timeseries))
	for _, ts := range res.Timeseries {
		labels := labelProtosToLabels(ts.Labels)
		if err := validateLabelsAndMetricName(labels); err != nil {
			return errSeriesSet{err: err}
		}

		series = append(series, &concreteSeries{
			labels:     labels,
			samples:    ts.Samples,
			histograms: ts.Histograms,
		})
	}
	if sortSeries {
		sort.Sort(byLabel(series))
	}
	return &concreteSeriesSet{
		series: series,
	}
}

type byLabel []storage.Series

func (a byLabel) Len() int           { return len(a) }
func (a byLabel) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byLabel) Less(i, j int) bool { return labels.Compare(a[i].Labels(), a[j].Labels()) < 0 }

// errSeriesSet implements storage.SeriesSet, just returning an error.
type errSeriesSet struct {
	err error
}

func (errSeriesSet) Next() bool {
	return false
}

func (errSeriesSet) At() storage.Series {
	return nil
}

func (e errSeriesSet) Err() error {
	return e.err
}

func (e errSeriesSet) Warnings() annotations.Annotations { return nil }

// concreteSeriesSet implements storage.SeriesSet.
type concreteSeriesSet struct {
	cur    int
	series []storage.Series
}

func (c *concreteSeriesSet) Next() bool {
	c.cur++
	return c.cur-1 < len(c.series)
}

func (c *concreteSeriesSet) At() storage.Series {
	return c.series[c.cur-1]
}

func (c *concreteSeriesSet) Err() error {
	return nil
}

func (c *concreteSeriesSet) Warnings() annotations.Annotations { return nil }

// concreteSeries implements storage.Series.
type concreteSeries struct {
	labels     labels.Labels
	samples    []prompb.Sample
	histograms []prompb.Histogram
}

func (c *concreteSeries) Labels() labels.Labels {
	return c.labels.Copy()
}

func (c *concreteSeries) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return newConcreteSeriersIterator(c)
}

// concreteSeriesIterator implements chunkenc.Iterator over the parallel
// float and histogram sample sequences carried in a concreteSeries. Both
// sequences are timestamp-ordered; Next() advances whichever has the smaller
// next-timestamp.
type concreteSeriesIterator struct {
	floatCur int
	histCur  int
	last     chunkenc.ValueType
	series   *concreteSeries
}

func newConcreteSeriersIterator(series *concreteSeries) chunkenc.Iterator {
	return &concreteSeriesIterator{
		floatCur: -1,
		histCur:  -1,
		series:   series,
	}
}

// Seek implements chunkenc.Iterator.
func (c *concreteSeriesIterator) Seek(t int64) chunkenc.ValueType {
	c.floatCur = sort.Search(len(c.series.samples), func(n int) bool {
		return c.series.samples[n].Timestamp >= t
	}) - 1
	c.histCur = sort.Search(len(c.series.histograms), func(n int) bool {
		return c.series.histograms[n].Timestamp >= t
	}) - 1
	return c.Next()
}

// At implements chunkenc.Iterator.
func (c *concreteSeriesIterator) At() (t int64, v float64) {
	s := c.series.samples[c.floatCur]
	return s.Timestamp, s.Value
}

// AtHistogram returns the current integer histogram. Only valid after Next/Seek
// returned ValHistogram.
func (c *concreteSeriesIterator) AtHistogram(_ *histogram.Histogram) (int64, *histogram.Histogram) {
	h := c.series.histograms[c.histCur]
	return h.Timestamp, h.ToIntHistogram()
}

// AtFloatHistogram returns the current histogram as a FloatHistogram. Valid
// after Next/Seek returned either ValHistogram or ValFloatHistogram.
func (c *concreteSeriesIterator) AtFloatHistogram(_ *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	h := c.series.histograms[c.histCur]
	return h.Timestamp, h.ToFloatHistogram()
}

// AtT implements chunkenc.Iterator.
func (c *concreteSeriesIterator) AtT() int64 {
	if c.last == chunkenc.ValHistogram || c.last == chunkenc.ValFloatHistogram {
		return c.series.histograms[c.histCur].Timestamp
	}
	return c.series.samples[c.floatCur].Timestamp
}

// Next implements chunkenc.Iterator.
func (c *concreteSeriesIterator) Next() chunkenc.ValueType {
	hasFloat := c.floatCur+1 < len(c.series.samples)
	hasHist := c.histCur+1 < len(c.series.histograms)

	switch {
	case !hasFloat && !hasHist:
		c.last = chunkenc.ValNone
		return chunkenc.ValNone
	case hasFloat && (!hasHist || c.series.samples[c.floatCur+1].Timestamp <= c.series.histograms[c.histCur+1].Timestamp):
		c.floatCur++
		c.last = chunkenc.ValFloat
		return chunkenc.ValFloat
	default:
		c.histCur++
		if c.series.histograms[c.histCur].IsFloatHistogram() {
			c.last = chunkenc.ValFloatHistogram
		} else {
			c.last = chunkenc.ValHistogram
		}
		return c.last
	}
}

// Err implements chunkenc.Iterator.
func (c *concreteSeriesIterator) Err() error {
	return nil
}

// validateLabelsAndMetricName validates the label names/values and metric
// names returned from remote read using the legacy (pre-UTF8) Prometheus
// rules. We deliberately don't honour model.NameValidationScheme here:
// promxy runs as a gateway across heterogeneous downstreams and we want
// rejection to be predictable regardless of whichever validation scheme
// happens to be globally configured at runtime.
func validateLabelsAndMetricName(ls labels.Labels) error {
	var validateErr error
	ls.Range(func(l labels.Label) {
		if validateErr != nil {
			return
		}
		if l.Name == labels.MetricName && !model.IsValidLegacyMetricName(l.Value) {
			validateErr = fmt.Errorf("invalid metric name: %v", l.Value)
			return
		}
		if !model.LabelName(l.Name).IsValidLegacy() {
			validateErr = fmt.Errorf("invalid label name: %v", l.Name)
			return
		}
		if !model.LabelValue(l.Value).IsValid() {
			validateErr = fmt.Errorf("invalid label value: %v", l.Value)
		}
	})
	return validateErr
}

func toLabelMatchers(matchers []*labels.Matcher) ([]*prompb.LabelMatcher, error) {
	pbMatchers := make([]*prompb.LabelMatcher, 0, len(matchers))
	for _, m := range matchers {
		var mType prompb.LabelMatcher_Type
		switch m.Type {
		case labels.MatchEqual:
			mType = prompb.LabelMatcher_EQ
		case labels.MatchNotEqual:
			mType = prompb.LabelMatcher_NEQ
		case labels.MatchRegexp:
			mType = prompb.LabelMatcher_RE
		case labels.MatchNotRegexp:
			mType = prompb.LabelMatcher_NRE
		default:
			return nil, fmt.Errorf("invalid matcher type")
		}
		pbMatchers = append(pbMatchers, &prompb.LabelMatcher{
			Type:  mType,
			Name:  m.Name,
			Value: m.Value,
		})
	}
	return pbMatchers, nil
}

func fromLabelMatchers(matchers []*prompb.LabelMatcher) ([]*labels.Matcher, error) {
	result := make([]*labels.Matcher, 0, len(matchers))
	for _, matcher := range matchers {
		var mtype labels.MatchType
		switch matcher.Type {
		case prompb.LabelMatcher_EQ:
			mtype = labels.MatchEqual
		case prompb.LabelMatcher_NEQ:
			mtype = labels.MatchNotEqual
		case prompb.LabelMatcher_RE:
			mtype = labels.MatchRegexp
		case prompb.LabelMatcher_NRE:
			mtype = labels.MatchNotRegexp
		default:
			return nil, fmt.Errorf("invalid matcher type")
		}
		matcher, err := labels.NewMatcher(mtype, matcher.Name, matcher.Value)
		if err != nil {
			return nil, err
		}
		result = append(result, matcher)
	}
	return result, nil
}

// MetricToLabelProtos builds a []*prompb.Label from a model.Metric
func MetricToLabelProtos(metric model.Metric) []prompb.Label {
	labels := make([]prompb.Label, 0, len(metric))
	for k, v := range metric {
		labels = append(labels, prompb.Label{
			Name:  string(k),
			Value: string(v),
		})
	}
	sort.Slice(labels, func(i int, j int) bool {
		return labels[i].Name < labels[j].Name
	})
	return labels
}

// LabelProtosToMetric unpack a []*prompb.Label to a model.Metric
func LabelProtosToMetric(labelPairs []*prompb.Label) model.Metric {
	metric := make(model.Metric, len(labelPairs))
	for _, l := range labelPairs {
		metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}
	return metric
}

func labelProtosToLabels(labelPairs []prompb.Label) labels.Labels {
	b := labels.NewScratchBuilder(len(labelPairs))
	for _, l := range labelPairs {
		b.Add(l.Name, l.Value)
	}
	b.Sort()
	return b.Labels()
}

func labelsToLabelsProto(ls labels.Labels) []prompb.Label {
	result := make([]prompb.Label, 0, ls.Len())
	ls.Range(func(l labels.Label) {
		result = append(result, prompb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	})
	return result
}

func labelsToMetric(ls labels.Labels) model.Metric {
	metric := make(model.Metric, ls.Len())
	ls.Range(func(l labels.Label) {
		metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	})
	return metric
}
