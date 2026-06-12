package promclient

import (
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"
)

// downgradeErrSeriesSet moves any error into the warnings and clears Err(), so
// a failing downstream is treated as a non-fatal warning.
type downgradeErrSeriesSet struct {
	storage.SeriesSet
}

func (d downgradeErrSeriesSet) Err() error { return nil }

func (d downgradeErrSeriesSet) Warnings() annotations.Annotations {
	w := d.SeriesSet.Warnings()
	if err := d.SeriesSet.Err(); err != nil {
		w = w.Add(err)
	}
	return w
}

// DowngradeErrSeriesSet returns ss with any error demoted to a warning.
func DowngradeErrSeriesSet(ss storage.SeriesSet) storage.SeriesSet {
	return downgradeErrSeriesSet{ss}
}

// warningsSeriesSet overrides Warnings() with a fixed set (used after merging
// HA members, where warnings are collected across all responses).
type warningsSeriesSet struct {
	storage.SeriesSet
	w annotations.Annotations
}

func (s warningsSeriesSet) Warnings() annotations.Annotations { return s.w }

// WithWarnings returns ss reporting w as its warnings.
func WithWarnings(ss storage.SeriesSet, w annotations.Annotations) storage.SeriesSet {
	return warningsSeriesSet{ss, w}
}

// MergeAnnotations unions b into a (a may be nil).
func MergeAnnotations(a, b annotations.Annotations) annotations.Annotations {
	for k, v := range b {
		if a == nil {
			a = annotations.Annotations{}
		}
		a[k] = v
	}
	return a
}

// errMapSeriesSet wraps a SeriesSet and transforms its Err() (fn receives nil
// when there is no error, and may return nil to clear it).
type errMapSeriesSet struct {
	storage.SeriesSet
	fn func(error) error
}

func (e *errMapSeriesSet) Err() error { return e.fn(e.SeriesSet.Err()) }

// MapErrSeriesSet returns ss with its error transformed by fn.
func MapErrSeriesSet(ss storage.SeriesSet, fn func(error) error) storage.SeriesSet {
	return &errMapSeriesSet{ss, fn}
}

// labelMapSeriesSet rewrites each series' labels via fn. A nil result from fn
// drops the series.
type labelMapSeriesSet struct {
	storage.SeriesSet
	fn func(labels.Labels) labels.Labels
}

func (l *labelMapSeriesSet) At() storage.Series {
	s := l.SeriesSet.At()
	return &labelMapSeries{Series: s, lbls: l.fn(s.Labels())}
}

type labelMapSeries struct {
	storage.Series
	lbls labels.Labels
}

func (s *labelMapSeries) Labels() labels.Labels { return s.lbls }

// MapLabelsSeriesSet returns ss with each series' labels rewritten by fn.
func MapLabelsSeriesSet(ss storage.SeriesSet, fn func(labels.Labels) labels.Labels) storage.SeriesSet {
	return &labelMapSeriesSet{SeriesSet: ss, fn: fn}
}
