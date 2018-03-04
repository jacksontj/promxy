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
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
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
	ServerGroups servergroup.ServerGroups
	Client *http.Client
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }

// QueryRange returns a list of series iterators for the selected
// time range and label matchers. The iterators need to be closed
// after usage.
func (h *ProxyQuerier) QueryRange(ctx context.Context, from, through model.Time, matchers ...*metric.LabelMatcher) ([]local.SeriesIterator, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"from":     from,
			"through":  through,
			"matchers": matchers,
			"took":     time.Now().Sub(start),
		}).Debug("QueryRange")
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
	values.Add("query", pql+fmt.Sprintf("[%ds]", int64(through.Sub(from).Seconds())+1))
	values.Add("time", through.String())

	result, err := h.ServerGroups.GetData(ctx, urlBase, values, h.Client)
	if err != nil {
		return nil, err
	}

	iterators := promclient.IteratorsForValue(result)
	returnIterators := make([]local.SeriesIterator, len(iterators))
	for i, item := range iterators {
		returnIterators[i] = item
	}
	return returnIterators, nil

}

// QueryInstant returns a list of series iterators for the selected
// instant and label matchers. The iterators need to be closed after usage.
func (h *ProxyQuerier) QueryInstant(ctx context.Context, ts model.Time, stalenessDelta time.Duration, matchers ...*metric.LabelMatcher) ([]local.SeriesIterator, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"ts":             ts,
			"stalenessDelta": stalenessDelta,
			"matchers":       matchers,
			"took":           time.Now().Sub(start),
		}).Debug("QueryInstant")
	}()

	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	// Create the query params
	values := url.Values{}
	values.Add("query", pql)
	values.Add("time", ts.String())
	values.Add("_", ts.Add(-stalenessDelta).String())

	result, err := h.ServerGroups.GetData(ctx, "/api/v1/query", values, h.Client)
	if err != nil {
		return nil, err
	}

	iterators := promclient.IteratorsForValue(result)
	returnIterators := make([]local.SeriesIterator, len(iterators))
	for i, item := range iterators {
		returnIterators[i] = item
	}
	return returnIterators, nil
}

// MetricsForLabelMatchers returns the metrics from storage that satisfy
// the given sets of label matchers. Each set of matchers must contain at
// least one label matcher that does not match the empty string. Otherwise,
// an empty list is returned. Within one set of matchers, the intersection
// of matching series is computed. The final return value will be the union
// of the per-set results. The times from and through are hints for the
// storage to optimize the search. The storage MAY exclude metrics that
// have no samples in the specified interval from the returned map. In
// doubt, specify model.Earliest for from and model.Latest for through.
func (h *ProxyQuerier) MetricsForLabelMatchers(ctx context.Context, from, through model.Time, matcherSets ...metric.LabelMatchers) ([]metric.Metric, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"from":        from,
			"through":     through,
			"matcherSets": matcherSets,
			"took":        time.Now().Sub(start),
		}).Debug("MetricsForLabelMatchers")
	}()

	// http://10.0.1.115:8082/api/v1/series?match[]=scrape_samples_scraped&start=1507432802&end=1507433102

	values := url.Values{}
	// Only add time ranges if they aren't the edges
	if from.After(model.Earliest) || through.Before(model.Latest) {
		values.Add("start", from.String())
		values.Add("end", through.String())
	}

	// Add matchers
	for _, matcherList := range matcherSets {
		pql, err := MatcherToString(matcherList)
		if err != nil {
			return nil, err
		}
		values.Add("match[]", pql)
	}

	result, err := h.ServerGroups.GetSeries(ctx, "/api/v1/series", values, h.Client)
	if err != nil {
		return nil, err
	}

	metrics := make([]metric.Metric, len(result.Data))
	for i, labelSet := range result.Data {
		metrics[i] = metric.Metric{
			Copied: true,
			Metric: model.Metric(labelSet),
		}
	}
	return metrics, nil
}

// TODO: remove? This was dropped in prometheus 2 -- so probably not worth implementing
// LastSampleForLabelMatchers returns the last samples that have been
// ingested for the time series matching the given set of label matchers.
// The label matching behavior is the same as in MetricsForLabelMatchers.
// All returned samples are between the specified cutoff time and now.
func (h *ProxyQuerier) LastSampleForLabelMatchers(ctx context.Context, cutoff model.Time, matcherSets ...metric.LabelMatchers) (model.Vector, error) {
	logrus.WithFields(logrus.Fields{
		"cutoff":      cutoff,
		"matcherSets": matcherSets,
	}).Debug("MetricsForLastSampleForLabelMatchersLabelMatchers")
	return nil, fmt.Errorf("Not implemented")
}

// Get all of the label values that are associated with a given label name.
func (h *ProxyQuerier) LabelValuesForLabelName(ctx context.Context, name model.LabelName) (model.LabelValues, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Now().Sub(start),
		}).Debug("LabelValuesForLabelName")
	}()

	result, err := h.ServerGroups.GetValuesForLabelName(ctx, "/api/v1/label/"+string(name)+"/values", h.Client)
	if err != nil {
		return nil, err
	}

	return result.Data, nil
}
