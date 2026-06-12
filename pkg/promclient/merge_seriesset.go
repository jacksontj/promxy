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
	return storage.NewListSeries(lbls, samples)
}

func mergeAntiAffinity(antiAffinity model.Time, preferMax bool) storage.VerticalSeriesMergeFunc {
	return func(series ...storage.Series) storage.Series {
		if len(series) == 0 {
			return nil
		}
		lbls := series[0].Labels()
		merged := seriesToSampleStream(series[0])
		for _, s := range series[1:] {
			merged, _ = promhttputil.MergeSampleStream(antiAffinity, merged, seriesToSampleStream(s), preferMax)
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
// preserving promxy's existing merge semantics.
func MergeSeriesSets(antiAffinity model.Time, preferMax bool, sets ...storage.SeriesSet) storage.SeriesSet {
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
	return storage.NewMergeSeriesSet(sorted, 0, mergeAntiAffinity(antiAffinity, preferMax))
}
