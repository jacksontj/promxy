package proxyquerier

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
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
	ServerGroups []*servergroup.ServerGroup
	// TODO: use
	Client *http.Client
	// TODO: support limits to the hosts we query
	// Configurable -- N hosts to query M required to complete
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
		}).Info("QueryRange")
	}()

	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	// Create the query params
	var urlBase string
	var call string
	values := url.Values{}

	// TODO: config (ideally step would be passed down :/ )
	MAX_DATAPOINTS := float64(500)
	SCRAPE_INTERVAL := float64(15)

	// If regular 15s scraping ends up with more than 500 points, switch to using a step
	// which gives us roughly that
	step := through.Sub(from).Seconds() / MAX_DATAPOINTS

	// TODO: config?
	// There is a hard-coded 5m "staleness" number in promql. As such you can't
	// have a step of longer than 5m otherwise you get holes in the graph
	if step > 200 {
		step = 200
	}

	// If our calculated step is lower than what we expect raw to be, lets get raw
	if step < (SCRAPE_INTERVAL * 2) {
		// We want to do a normal query (for raw data)
		urlBase = "%s/api/v1/query"
		call = "query"

		// We want to grab only the raw datapoints, so we do that through the query interface
		// passing in a duration that is at least as long as ours (the added second is to deal
		// with any rounding error etc since the duration is a floating point and we are casting
		// to an int64
		values.Add("query", pql+fmt.Sprintf("[%ds]", int64(through.Sub(from).Seconds())+1))
		values.Add("time", through.String())

	} else { // If step is significanltly less (2x) we'll do that instead
		// We want to do a queryrange to rely on step to reduce number of datapoints
		urlBase = "%s/api/v1/query_range"
		call = "query_range"

		values.Add("query", pql)
		values.Add("start", from.String())
		values.Add("end", through.String())
		values.Add("step", strconv.FormatFloat(step, 'f', -1, 64))
	}

	var result model.Value

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup.Targets() {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf(urlBase, server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, parsedUrl *url.URL, ls model.LabelSet, retChan chan interface{}) {
				start := time.Now()
				serverResult, err := promclient.GetData(ctx, parsedUrl.String(), h.Client, ls)
				took := time.Now().Sub(start)
				var ret interface{}
				if err != nil {
					ret = err
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, call, "error").Observe(float64(took))
				} else {
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, call, "success").Observe(float64(took))
					ret = serverResult
				}

				select {
				case retChan <- ret:
					return
				case <-ctx.Done():
					return
				}
			}(childContext, parsedUrl, serverGroup.Cfg.Labels, retChan)
		}
	}

	errCount := 0
	for i := 0; i < retCount; i++ {
		select {
		// If the context was closed, we are erroring out (usually client disconnect)
		case <-ctx.Done():
			return nil, ctx.Err()
		// Otherwise we are waiting on a return
		case ret := <-retChan:
			switch retTyped := ret.(type) {
			// If there was an error we'll just continue
			case error:
				// Don't stop on error, just incr counter
				errCount++
			case *promhttputil.Response:
				// TODO: check response code, how do we want to handle it?
				if retTyped.Status != promhttputil.StatusSuccess {
					errCount++
					continue
				}

				// TODO: what to do in failure
				qData, ok := retTyped.Data.(*promhttputil.QueryData)
				if !ok {
					errCount++
					continue
				}

				// TODO: check qData.ResultType

				if result == nil {
					result = qData.Result
				} else {
					result, err = promhttputil.MergeValues(result, qData.Result)
					if err != nil {
						return nil, err
					}
				}
			}
		}
	}

	if errCount == retCount {
		return nil, fmt.Errorf("Unable to fetch from downstream servers")
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
		}).Info("QueryInstant")
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

	var result model.Value

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup.Targets() {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/query", server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, parsedUrl *url.URL, ls model.LabelSet, retChan chan interface{}) {
				start := time.Now()
				serverResult, err := promclient.GetData(ctx, parsedUrl.String(), h.Client, ls)
				took := time.Now().Sub(start)
				var ret interface{}
				if err != nil {
					ret = err
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "query", "success").Observe(float64(took))
				} else {
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "query", "error").Observe(float64(took))
					ret = serverResult
				}
				select {
				case retChan <- ret:
					return
				case <-ctx.Done():
					return
				}
			}(childContext, parsedUrl, serverGroup.Cfg.Labels, retChan)
		}
	}

	errCount := 0
	for i := 0; i < retCount; i++ {
		select {
		// If the context was closed, we are erroring out (usually client disconnect)
		case <-ctx.Done():
			return nil, ctx.Err()
		// Otherwise we are waiting on a return
		case ret := <-retChan:
			switch retTyped := ret.(type) {
			// If there was an error we'll just continue
			case error:
				// Don't stop on error, just incr counter
				errCount++
			case *promhttputil.Response:
				// TODO: check response code, how do we want to handle it?
				if retTyped.Status != promhttputil.StatusSuccess {
					continue
				}

				// TODO: what to do in failure
				qData, ok := retTyped.Data.(*promhttputil.QueryData)
				if !ok {
					continue
				}

				// TODO: check qData.ResultType

				if result == nil {
					result = qData.Result
				} else {
					result, err = promhttputil.MergeValues(result, qData.Result)
					if err != nil {
						return nil, err
					}
				}
			}
		}
	}

	if errCount == retCount {
		return nil, fmt.Errorf("Unable to fetch from downstream servers")
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
		}).Info("MetricsForLabelMatchers")
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

	result := &promclient.SeriesResult{}

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup.Targets() {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/series", server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, retChan chan interface{}) {
				start := time.Now()
				serverResult, err := promclient.GetSeries(ctx, parsedUrl.String(), h.Client)
				took := time.Now().Sub(start)
				var ret interface{}
				if err != nil {
					ret = err
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "series", "error").Observe(float64(took))
				} else {
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "series", "success").Observe(float64(took))
					ret = serverResult
				}
				select {
				case retChan <- ret:
					return
				case <-ctx.Done():
					return
				}
			}(childContext, retChan)
		}
	}

	errCount := 0
	for i := 0; i < retCount; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-retChan:
			switch retTyped := ret.(type) {
			case error:
				errCount++
			case *promclient.SeriesResult:
				// TODO check status
				if err := result.Merge(retTyped); err != nil {
					// TODO: be smarter about checking if we have enough from a specific server_group
					errCount++
				}
			}
		}
	}

	if errCount == retCount {
		return nil, fmt.Errorf("Unable to fetch from downstream servers")
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
	}).Info("MetricsForLastSampleForLabelMatchersLabelMatchers")
	return nil, fmt.Errorf("Not implemented")
}

// Get all of the label values that are associated with a given label name.
func (h *ProxyQuerier) LabelValuesForLabelName(ctx context.Context, name model.LabelName) (model.LabelValues, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Now().Sub(start),
		}).Info("LabelValuesForLabelName")
	}()

	result := &promclient.LabelResult{}

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup.Targets() {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/label/%s/values", server, name))
			if err != nil {
				return nil, err
			}

			go func(ctx context.Context, retChan chan interface{}) {
				start := time.Now()
				serverResult, err := promclient.GetValuesForLabelName(ctx, parsedUrl.String(), h.Client)
				took := time.Now().Sub(start)
				var ret interface{}
				if err != nil {
					ret = err
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "label_values", "error").Observe(float64(took))
				} else {
					proxyQuerierSummary.WithLabelValues(parsedUrl.Host, "label_values", "success").Observe(float64(took))
					ret = serverResult
				}
				select {
				case retChan <- ret:
					return
				case <-ctx.Done():
					return
				}
			}(childContext, retChan)
		}
	}

	errCount := 0

	for i := 0; i < retCount; i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-retChan:
			switch retTyped := ret.(type) {
			case error:
				errCount++
			case *promclient.LabelResult:
				// TODO check status
				if err := result.Merge(retTyped); err != nil {
					// TODO: be smarter about checking if we have enough from a specific server_group
					errCount++
				}
			}
		}
	}

	if errCount == retCount {
		return nil, fmt.Errorf("Unable to fetch from downstream servers")
	}

	return result.Data, nil
}
