package promclient

import (
	"context"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// StepAlignClient re-stamps QueryRange sample timestamps from the epoch step
// grid (k*step, as returned by step-aligning backends such as Mimir/Cortex)
// onto the grid implied by the request start (start + j*step). It is meant to
// be enabled per-server-group, only for backends that actually snap
// query_range results to the epoch grid.
//
// The shift is purely additive: r = start mod step, applied forward (+r), so a
// sample the backend computed at k*step is reported at the next requested grid
// point k*step + r. Forward-only (never backward) keeps it causal -- we never
// surface a value as of a time later than the timestamp it is reported at.
//
// Why this is the right reference frame: promxy pushes each leaf selector down
// as a QueryRange with Start = evalStart - offset, then the local engine looks
// the returned samples back up at (evalGrid - offset) == this request's Start
// grid. So aligning the output to r.Start's phase is exactly what the engine
// expects, regardless of any query offset.
type StepAlignClient struct {
	API
}

// QueryRange performs a query for the given range, re-stamping the returned
// samples onto the request's start+step grid. It is a no-op when the request is
// already on the grid (start % step == 0) or the step is non-positive.
func (c *StepAlignClient) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	ss := c.API.QueryRange(ctx, query, r)
	if r.Step <= 0 {
		return ss
	}
	stepMs := r.Step.Milliseconds()
	if stepMs <= 0 {
		return ss
	}
	shift := ((r.Start.UnixMilli() % stepMs) + stepMs) % stepMs
	if shift == 0 {
		return ss
	}
	return &timeShiftSeriesSet{SeriesSet: ss, shift: shift}
}

// timeShiftSeriesSet lazily adds a fixed millisecond shift to every sample
// timestamp of every series. It does not materialize -- the shift is applied in
// the iterator so the result stays streamable and re-iterable like its source.
type timeShiftSeriesSet struct {
	storage.SeriesSet
	shift int64
}

func (s *timeShiftSeriesSet) At() storage.Series {
	return &timeShiftSeries{Series: s.SeriesSet.At(), shift: s.shift}
}

type timeShiftSeries struct {
	storage.Series
	shift int64
}

func (s *timeShiftSeries) Iterator(it chunkenc.Iterator) chunkenc.Iterator {
	inner := s.Series.Iterator(nil)
	if r, ok := it.(*timeShiftIterator); ok {
		r.Iterator = inner
		r.shift = s.shift
		return r
	}
	return &timeShiftIterator{Iterator: inner, shift: s.shift}
}

type timeShiftIterator struct {
	chunkenc.Iterator
	shift int64
}

func (it *timeShiftIterator) At() (int64, float64) {
	t, v := it.Iterator.At()
	return t + it.shift, v
}

func (it *timeShiftIterator) AtHistogram(h *histogram.Histogram) (int64, *histogram.Histogram) {
	t, h2 := it.Iterator.AtHistogram(h)
	return t + it.shift, h2
}

func (it *timeShiftIterator) AtFloatHistogram(fh *histogram.FloatHistogram) (int64, *histogram.FloatHistogram) {
	t, fh2 := it.Iterator.AtFloatHistogram(fh)
	return t + it.shift, fh2
}

func (it *timeShiftIterator) AtT() int64 {
	return it.Iterator.AtT() + it.shift
}

// Seek positions at the first sample whose *shifted* timestamp is >= t, which
// is the first underlying sample with timestamp >= t-shift.
func (it *timeShiftIterator) Seek(t int64) chunkenc.ValueType {
	return it.Iterator.Seek(t - it.shift)
}
