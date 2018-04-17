package proxyquerier

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/servergroup"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
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
	Client       *http.Client
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
	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	// Create the query params
	values := url.Values{}

	// We want to do a normal query (for raw data)
	urlBase := "/api/v1/query"

	// We want to grab only the raw datapoints, so we do that through the query interface
	// passing in a duration that is at least as long as ours (the added second is to deal
	// with any rounding error etc since the duration is a floating point and we are casting
	// to an int64
	values.Add("query", pql+fmt.Sprintf("[%ds]", int64(h.End.Sub(h.Start).Seconds())+1))
	values.Add("time", model.Time(timestamp.FromTime(h.End)).String())

	result, err := h.ServerGroups.GetData(h.Ctx, urlBase, values, h.Client)
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

	result, err := h.ServerGroups.GetValuesForLabelName(h.Ctx, "/api/v1/label/"+string(name)+"/values", h.Client)
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
