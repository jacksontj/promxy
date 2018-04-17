package proxyquerier

import (
	"github.com/prometheus/prometheus/storage"
)

func NewSeriesSet(series []storage.Series) *SeriesSet {
	return &SeriesSet{
		series: series,
	}
}

type SeriesSet struct {
	offset int // 0 means we haven't seen anything
	series []storage.Series
}

func (s *SeriesSet) Next() bool {
	if s.offset < len(s.series) {
		s.offset++
		return true
	}
	return false
}

func (s *SeriesSet) At() storage.Series {
	return s.series[s.offset-1]
}

func (s *SeriesSet) Err() error {
	return nil
}
