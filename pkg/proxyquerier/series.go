package proxyquerier

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	"github.com/jacksontj/promxy/pkg/promclient"
)

// Series implements prometheus' Series interface
type Series struct {
	It *promclient.SeriesIterator
}

// Labels for this seris
func (s *Series) Labels() labels.Labels {
	return s.It.Labels()
}

// Iterator returns an iterator over the series
func (s *Series) Iterator() chunkenc.Iterator {
	return s.It
}
