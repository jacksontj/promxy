package proxyquerier

import (
	"context"
	"time"

	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/servergroup"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

var (
	proxyQuerierSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "proxy_querier_request",
		Help: "Summary of proxyquerier calls to downstreams",
	}, []string{"host", "call", "status"})
)

func init() {
	prometheus.MustRegister(proxyQuerierSummary)
}

type ProxyQuerier struct {
	Ctx          context.Context
	Start        time.Time
	End          time.Time
	ServerGroups servergroup.ServerGroups

	Cfg *proxyconfig.PromxyConfig
}

// Select returns a set of series that matches the given label matchers.
func (h *ProxyQuerier) Select(selectParams *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"selectParams": selectParams,
			"matchers":     matchers,
			"took":         time.Now().Sub(start),
		}).Debug("Select")
	}()

	result, err := h.ServerGroups.GetValue(h.Ctx, h.Start, h.End, matchers)
	if err != nil {
		return nil, err
	}

	iterators := promclient.IteratorsForValue(result)

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &Series{iterator}
	}

	return NewSeriesSet(series), nil
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(name string) ([]string, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Now().Sub(start),
		}).Debug("LabelValues")
	}()

	result, err := h.ServerGroups.GetValuesForLabelName(h.Ctx, "/api/v1/label/"+string(name)+"/values")
	if err != nil {
		return nil, err
	}

	ret := make([]string, len(result.Data))
	for i, r := range result.Data {
		ret[i] = string(r)
	}

	return ret, nil
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }
