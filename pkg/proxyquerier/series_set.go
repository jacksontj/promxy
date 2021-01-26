package proxyquerier

import (
	"github.com/prometheus/prometheus/storage"
)

// NewSeriesSet returns a SeriesSet for the given series
func NewSeriesSet(series []storage.Series, warnings storage.Warnings, err error) *SeriesSet {
	return &SeriesSet{
		series: series,
	}
}

// SeriesSet implements prometheus' SeriesSet interface
type SeriesSet struct {
	offset int // 0 means we haven't seen anything
	series []storage.Series

	err      error
	warnings storage.Warnings
}

// Next will attempt to move the iterator up
func (s *SeriesSet) Next() bool {
	if s.offset < len(s.series) {
		s.offset++
		return true
	}
	return false
}

// At returns the current Series for this iterator
func (s *SeriesSet) At() storage.Series {
	return s.series[s.offset-1]
}

// Err returns any error found in this iterator
func (s *SeriesSet) Err() error {
	return s.err
}

// A collection of warnings for the whole set.
// Warnings could be return even iteration has not failed with error.
func (s *SeriesSet) Warnings() storage.Warnings {
	return s.warnings
}
