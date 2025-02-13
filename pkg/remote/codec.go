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
	"math"
	"net/http"
	"sort"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
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

		for iter.Next() != chunkenc.ValNone {
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
		}
		if err := iter.Err(); err != nil {
			return nil, err
		}

		resp.Timeseries = append(resp.Timeseries, &prompb.TimeSeries{
			Labels:  labelsToLabelsProto(series.Labels()),
			Samples: samples,
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
	return labels.New(c.labels...)
}

func (c *concreteSeries) Iterator(it chunkenc.Iterator) chunkenc.Iterator {
	if csi, ok := it.(*concreteSeriesIterator); ok {
		csi.reset(c)
		return csi
	}
	return newConcreteSeriersIterator(c)
}

// concreteSeriesIterator implements storage.SeriesIterator.
type concreteSeriesIterator struct {
	cur           int
	histogramsCur int
	curValType    chunkenc.ValueType
	series        *concreteSeries
}

func (c *concreteSeriesIterator) reset(series *concreteSeries) {
	c.cur = -1
	c.histogramsCur = -1
	c.curValType = chunkenc.ValNone
	c.series = series
}

func newConcreteSeriersIterator(series *concreteSeries) chunkenc.Iterator {
	return &concreteSeriesIterator{
		cur:           -1,
		histogramsCur: -1,
		curValType:    chunkenc.ValNone,
		series:        series,
	}
}

func getHistogramValType(h *prompb.Histogram) chunkenc.ValueType {
	if h.IsFloatHistogram() {
		return chunkenc.ValFloatHistogram
	}
	return chunkenc.ValHistogram
}

// At implements chunkenc.Iterator.
func (c *concreteSeriesIterator) At() (t int64, v float64) {
	if c.curValType != chunkenc.ValFloat {
		panic("iterator is not on a float sample")
	}
	s := c.series.samples[c.cur]
	return s.Timestamp, s.Value
}

// Seek implements storage.SeriesIterator.
func (c *concreteSeriesIterator) Seek(t int64) chunkenc.ValueType {
	if c.cur == -1 {
		c.cur = 0
	}
	if c.histogramsCur == -1 {
		c.histogramsCur = 0
	}
	if c.cur >= len(c.series.samples) && c.histogramsCur >= len(c.series.histograms) {
		return chunkenc.ValNone
	}

	// No-op check.
	if (c.curValType == chunkenc.ValFloat && c.series.samples[c.cur].Timestamp >= t) ||
		((c.curValType == chunkenc.ValHistogram || c.curValType == chunkenc.ValFloatHistogram) && c.series.histograms[c.histogramsCur].Timestamp >= t) {
		return c.curValType
	}

	c.curValType = chunkenc.ValNone

	// Binary search between current position and end for both float and histograms samples.
	c.cur += sort.Search(len(c.series.samples)-c.cur, func(n int) bool {
		return c.series.samples[n+c.cur].Timestamp >= t
	})
	c.histogramsCur += sort.Search(len(c.series.histograms)-c.histogramsCur, func(n int) bool {
		return c.series.histograms[n+c.histogramsCur].Timestamp >= t
	})
	switch {
	case c.cur < len(c.series.samples) && c.histogramsCur < len(c.series.histograms):
		// If float samples and histogram samples have overlapping timestamps prefer the float samples.
		if c.series.samples[c.cur].Timestamp <= c.series.histograms[c.histogramsCur].Timestamp {
			c.curValType = chunkenc.ValFloat
		} else {
			c.curValType = getHistogramValType(&c.series.histograms[c.histogramsCur])
		}
		// When the timestamps do not overlap the cursor for the non-selected sample type has advanced too
		// far; we decrement it back down here.
		if c.series.samples[c.cur].Timestamp != c.series.histograms[c.histogramsCur].Timestamp {
			if c.curValType == chunkenc.ValFloat {
				c.histogramsCur--
			} else {
				c.cur--
			}
		}
	case c.cur < len(c.series.samples):
		c.curValType = chunkenc.ValFloat
	case c.histogramsCur < len(c.series.histograms):
		c.curValType = getHistogramValType(&c.series.histograms[c.histogramsCur])
	}
	return c.curValType
}

// AtHistogram implements chunkenc.Iterator.
func (c *concreteSeriesIterator) AtHistogram(*histogram.Histogram) (int64, *histogram.Histogram) {
	if c.curValType != chunkenc.ValHistogram {
		panic("iterator is not on an integer histogram sample")
	}
	h := c.series.histograms[c.histogramsCur]
	return h.Timestamp, h.ToIntHistogram()
}

// AtFloatHistogram implements chunkenc.Iterator.
func (c *concreteSeriesIterator) AtFloatHistogram(*histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	if c.curValType == chunkenc.ValHistogram || c.curValType == chunkenc.ValFloatHistogram {
		fh := c.series.histograms[c.histogramsCur]
		return fh.Timestamp, fh.ToFloatHistogram() // integer will be auto-converted.
	}
	panic("iterator is not on a histogram sample")
}

// AtT implements chunkenc.Iterator.
func (c *concreteSeriesIterator) AtT() int64 {
	if c.curValType == chunkenc.ValHistogram || c.curValType == chunkenc.ValFloatHistogram {
		return c.series.histograms[c.histogramsCur].Timestamp
	}
	return c.series.samples[c.cur].Timestamp
}

const noTS = int64(math.MaxInt64)

func (c *concreteSeriesIterator) Next() chunkenc.ValueType {
	peekFloatTS := noTS
	if c.cur+1 < len(c.series.samples) {
		peekFloatTS = c.series.samples[c.cur+1].Timestamp
	}
	peekHistTS := noTS
	if c.histogramsCur+1 < len(c.series.histograms) {
		peekHistTS = c.series.histograms[c.histogramsCur+1].Timestamp
	}
	c.curValType = chunkenc.ValNone
	switch {
	case peekFloatTS < peekHistTS:
		c.cur++
		c.curValType = chunkenc.ValFloat
	case peekHistTS < peekFloatTS:
		c.histogramsCur++
		c.curValType = chunkenc.ValHistogram
	case peekFloatTS == int64(math.MaxInt64) && peekHistTS == int64(math.MaxInt64):
		// This only happens when the iterator is exhausted; we set the cursors off the end to prevent
		// Seek() from returning anything afterwards.
		c.cur = len(c.series.samples)
		c.histogramsCur = len(c.series.histograms)
	default:
		// Prefer float samples to histogram samples if there's a conflict. We advance the cursor for histograms
		// anyway otherwise the histogram sample will get selected on the next call to Next().
		c.cur++
		c.histogramsCur++
		c.curValType = chunkenc.ValFloat
	}
	return c.curValType
}

// Err implements storage.SeriesIterator.
func (c *concreteSeriesIterator) Err() error {
	return nil
}

// validateLabelsAndMetricName validates the label names/values and metric names returned from remote read.
func validateLabelsAndMetricName(ls labels.Labels) error {
	for _, l := range ls {
		if l.Name == labels.MetricName && !model.IsValidMetricName(model.LabelValue(l.Value)) {
			return fmt.Errorf("invalid metric name: %v", l.Value)
		}
		if !model.LabelName(l.Name).IsValid() {
			return fmt.Errorf("invalid label name: %v", l.Name)
		}
		if !model.LabelValue(l.Value).IsValid() {
			return fmt.Errorf("invalid label value: %v", l.Value)
		}
	}
	return nil
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
	result := make(labels.Labels, 0, len(labelPairs))
	for _, l := range labelPairs {
		result = append(result, labels.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	sort.Sort(result)
	return result
}

func labelsToLabelsProto(labels labels.Labels) []prompb.Label {
	result := make([]prompb.Label, 0, len(labels))
	for _, l := range labels {
		result = append(result, prompb.Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	return result
}

func labelsToMetric(ls labels.Labels) model.Metric {
	metric := make(model.Metric, len(ls))
	for _, l := range ls {
		metric[model.LabelName(l.Name)] = model.LabelValue(l.Value)
	}
	return metric
}
