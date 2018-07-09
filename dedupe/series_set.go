package dedupe

import "github.com/prometheus/prometheus/storage"

func NewConcurrentSeriesSet(set storage.SeriesSet) *ConcurrentSeriesSet {
	c := &ConcurrentSeriesSet{
		sets:   make([]storage.Series, 0),
		offset: -1,
	}

	for set.Next() {
		c.sets = append(c.sets, set.At())
	}

	if err := set.Err(); err != nil {
		c.err = err
	}
	return c
}

type ConcurrentSeriesSet struct {
	sets   []storage.Series
	err    error
	offset int
}

func (c *ConcurrentSeriesSet) Clone() *ConcurrentSeriesSet {
	return &ConcurrentSeriesSet{
		sets:   c.sets,
		offset: -1,
	}
}

func (c *ConcurrentSeriesSet) Next() bool {
	if c.offset < (len(c.sets) - 1) {
		c.offset++
		return true
	} else {
		return false
	}
}
func (c *ConcurrentSeriesSet) At() storage.Series {
	return c.sets[c.offset]
}

func (c *ConcurrentSeriesSet) Err() error {
	return c.err
}
