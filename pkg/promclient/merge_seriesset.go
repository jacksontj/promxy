package promclient

import (
	"sort"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// This adapts promxy's HA anti-affinity merge to storage.SeriesSet. Rather than
// re-derive the (subtle, heavily-tested) anti-affinity logic, it reuses
// promhttputil.MergeSampleStream verbatim by converting each same-labeled
// series to a model.SampleStream, folding, and converting back. Histograms
// round-trip losslessly through the floatHistogram pin.
//
// TODO(seriesset-refactor): port MergeSampleStream to operate on samples
// directly so the model.SampleStream round-trip here can be removed.

func labelsToMetric(ls labels.Labels) model.Metric {
	m := make(model.Metric, ls.Len())
	ls.Range(func(l labels.Label) { m[model.LabelName(l.Name)] = model.LabelValue(l.Value) })
	return m
}

func metricLabels(m model.Metric) labels.Labels {
	b := labels.NewScratchBuilder(len(m))
	for k, v := range m {
		b.Add(string(k), string(v))
	}
	b.Sort()
	return b.Labels()
}

// ModelValueToSeriesSet converts a model.Value (from the remote_read path or a
// client_golang fallback) into a storage.SeriesSet. It is the inverse of the
// streaming decode and exists only for the non-JSON paths that still produce
// model.Value.
func ModelValueToSeriesSet(v model.Value, warnings annotations.Annotations, err error) storage.SeriesSet {
	if err != nil {
		return promapi.NewSeriesSet(nil, warnings, err)
	}
	var series []storage.Series
	switch tv := v.(type) {
	case model.Vector:
		for _, s := range tv {
			ss := &model.SampleStream{Metric: s.Metric}
			if s.Histogram != nil {
				ss.Histograms = []model.SampleHistogramPair{{Timestamp: s.Timestamp, Histogram: s.Histogram}}
			} else {
				ss.Values = []model.SamplePair{{Timestamp: s.Timestamp, Value: s.Value}}
			}
			series = append(series, sampleStreamToSeries(ss, metricLabels(s.Metric)))
		}
	case model.Matrix:
		for _, ss := range tv {
			series = append(series, sampleStreamToSeries(ss, metricLabels(ss.Metric)))
		}
	case *model.Scalar:
		series = append(series, promapi.NewSeries(labels.EmptyLabels(),
			[]chunks.Sample{promapi.FloatSample(int64(tv.Timestamp), float64(tv.Value))}))
	}
	return promapi.NewSeriesSet(series, warnings, nil)
}

func seriesToSampleStream(s storage.Series) *model.SampleStream {
	ss := &model.SampleStream{Metric: labelsToMetric(s.Labels())}
	it := s.Iterator(nil)
	for {
		switch it.Next() {
		case chunkenc.ValNone:
			return ss
		case chunkenc.ValFloat:
			t, v := it.At()
			ss.Values = append(ss.Values, model.SamplePair{Timestamp: model.Time(t), Value: model.SampleValue(v)})
		case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
			t, fh := it.AtFloatHistogram(nil)
			ss.Histograms = append(ss.Histograms, model.SampleHistogramPair{Timestamp: model.Time(t), Histogram: floatHistogramToSampleHistogram(fh)})
		}
	}
}

func sampleStreamToSeries(ss *model.SampleStream, lbls labels.Labels) storage.Series {
	samples := make([]chunks.Sample, 0, len(ss.Values)+len(ss.Histograms))
	for _, p := range ss.Values {
		samples = append(samples, promapi.FloatSample(int64(p.Timestamp), float64(p.Value)))
	}
	for _, p := range ss.Histograms {
		samples = append(samples, promapi.HistogramSample(int64(p.Timestamp), sampleHistogramToFloatHistogram(p.Histogram)))
	}
	sort.SliceStable(samples, func(i, j int) bool { return samples[i].T() < samples[j].T() })
	return promapi.NewSeries(lbls, samples)
}

// materializeSeriesSet drains ss into an in-memory SeriesSet, copying every
// sample. This decouples the result from the source's lifecycle -- required for
// the remote_read streaming path, whose lazy ChunkedSeriesSet reads from an HTTP
// body that is closed (and whose context is canceled) the moment iteration
// would otherwise happen, after GetValue has already returned.
func materializeSeriesSet(ss storage.SeriesSet) storage.SeriesSet {
	var series []storage.Series
	for ss.Next() {
		s := ss.At()
		lbls := s.Labels().Copy()
		var samples []chunks.Sample
		it := s.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				t, v := it.At()
				samples = append(samples, promapi.FloatSample(t, v))
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				t, fh := it.AtFloatHistogram(nil)
				samples = append(samples, promapi.HistogramSample(t, fh))
			}
		}
		if err := it.Err(); err != nil {
			return promapi.NewSeriesSet(nil, ss.Warnings(), err)
		}
		series = append(series, promapi.NewSeries(lbls, samples))
	}
	return promapi.NewSeriesSet(series, ss.Warnings(), ss.Err())
}

// SeriesSetToMatrix materializes a storage.SeriesSet into a model.Matrix, for
// the few call sites that still operate on model.Value.
func SeriesSetToMatrix(ss storage.SeriesSet) (model.Matrix, error) {
	var m model.Matrix
	for ss.Next() {
		m = append(m, seriesToSampleStream(ss.At()))
	}
	return m, ss.Err()
}

func mergeAntiAffinity(antiAffinity model.Time, dynamic bool, preferMax bool) storage.VerticalSeriesMergeFunc {
	return func(series ...storage.Series) storage.Series {
		if len(series) == 0 {
			return nil
		}
		lbls := series[0].Labels()
		merged := seriesToSampleStream(series[0])
		for _, s := range series[1:] {
			merged, _ = promhttputil.MergeSampleStream(antiAffinity, dynamic, merged, seriesToSampleStream(s), preferMax)
		}
		return sampleStreamToSeries(merged, lbls)
	}
}

// sortedSeriesSet materializes ss and returns it sorted by labels, which
// storage.NewMergeSeriesSet requires of its inputs. Warnings and errors are
// preserved.
func sortedSeriesSet(ss storage.SeriesSet) storage.SeriesSet {
	var series []storage.Series
	for ss.Next() {
		series = append(series, ss.At())
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.Compare(series[i].Labels(), series[j].Labels()) < 0
	})
	return promapi.NewSeriesSet(series, ss.Warnings(), ss.Err())
}

// MergeSeriesSets merges HA-member SeriesSets with anti-affinity dedup,
// preserving promxy's existing merge semantics. When dynamic is true the
// anti-affinity buffer is inferred per series from inter-sample spacing (with
// antiAffinity as the fallback); see promhttputil.MergeSampleStream and #734.
func MergeSeriesSets(antiAffinity model.Time, dynamic bool, preferMax bool, sets ...storage.SeriesSet) storage.SeriesSet {
	if len(sets) == 0 {
		return promapi.NewSeriesSet(nil, annotations.Annotations{}, nil)
	}
	if len(sets) == 1 {
		return sets[0]
	}
	sorted := make([]storage.SeriesSet, len(sets))
	for i, s := range sets {
		sorted[i] = sortedSeriesSet(s)
	}
	// limit 0 = unlimited; the merge func applies anti-affinity to same-labeled
	// series across the sets.
	return storage.NewMergeSeriesSet(sorted, 0, mergeAntiAffinity(antiAffinity, dynamic, preferMax))
}
