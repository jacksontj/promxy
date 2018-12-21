package proxyquerier

import (
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/promclient"
)

type Series struct {
	It *promclient.SeriesIterator
}

func (s *Series) Labels() labels.Labels {
	return s.It.Labels()
}

func (s *Series) Iterator() storage.SeriesIterator {
	return s.It
}
