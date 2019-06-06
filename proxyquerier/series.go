package proxyquerier

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/promclient"
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
func (s *Series) Iterator() storage.SeriesIterator {
	return s.It
}
