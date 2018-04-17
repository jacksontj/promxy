package proxyquerier

import (
	"github.com/jacksontj/promxy/promclient"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
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
