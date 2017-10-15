package proxyquerier

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
)

type ProxyQuerier struct {
	ServerGroups [][]string
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
	// TODO: move to logging
	fmt.Printf("QueryRange: from=%v through=%v matchers=%v\n", from, through, matchers)

	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	// Create the query params
	values := url.Values{}
	values.Add("query", pql)
	values.Add("start", from.String())
	values.Add("end", through.String())
	// TODO: how do we pick this? configurable or something based on time range?
	// if we want to calculate irate for example, then we'd want more, right?
	if step := ctx.Value("step"); step != nil {
		stepInterval := step.(time.Duration)
		values.Add("step", (stepInterval / 2).String())
	} else {
		values.Add("step", "1")
	}

	var result model.Value

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/query_range", server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, retChan chan interface{}) {
				serverResult, err := promclient.GetData(ctx, parsedUrl.String())
				var ret interface{}
				if err != nil {
					ret = err
				} else {
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

// QueryInstant returns a list of series iterators for the selected
// instant and label matchers. The iterators need to be closed after usage.
func (h *ProxyQuerier) QueryInstant(ctx context.Context, ts model.Time, stalenessDelta time.Duration, matchers ...*metric.LabelMatcher) ([]local.SeriesIterator, error) {
	// TODO: move to logging
	fmt.Printf("QueryInstant: ts=%v stalenessDelta=%v matchers=%v\n", ts, stalenessDelta, matchers)

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
		for _, server := range serverGroup {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/query", server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, retChan chan interface{}) {
				serverResult, err := promclient.GetData(ctx, parsedUrl.String())
				var ret interface{}
				if err != nil {
					ret = err
				} else {
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
	fmt.Printf("MetricsForLabelMatchers: from=%v through=%v matcherSets=%v\n", from, through, matcherSets)
	// http://10.0.1.115:8082/api/v1/series?match[]=scrape_samples_scraped&start=1507432802&end=1507433102

	// TODO: check on this? For now the assumption is that we can merge all of the lists
	matchers := make([]*metric.LabelMatcher, 0, len(matcherSets))
	for _, matcherList := range matcherSets {
		matchers = append(matchers, matcherList...)
	}

	pql, err := MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	values := url.Values{}
	values.Add("match[]", pql)
	values.Add("start", from.String())
	values.Add("end", through.String())

	result := &promclient.SeriesResult{}

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/series", server))
			if err != nil {
				return nil, err
			}
			parsedUrl.RawQuery = values.Encode()

			go func(ctx context.Context, retChan chan interface{}) {
				serverResult, err := promclient.GetSeries(ctx, parsedUrl.String())
				var ret interface{}
				if err != nil {
					ret = err
				} else {
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
					return nil, err
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
	fmt.Printf("LastSampleForLabelMatchers: cutoff=%v matcherSets=%v\n", cutoff, matcherSets)
	return nil, fmt.Errorf("Not implemented")
}

// Get all of the label values that are associated with a given label name.
func (h *ProxyQuerier) LabelValuesForLabelName(ctx context.Context, name model.LabelName) (model.LabelValues, error) {
	fmt.Printf("LabelValuesForLabelName: name=%v\n", name)

	result := &promclient.LabelResult{}

	retChan := make(chan interface{})
	retCount := 0
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	// Query each in the groups and get data
	for _, serverGroup := range h.ServerGroups {
		for _, server := range serverGroup {
			retCount++

			parsedUrl, err := url.Parse(fmt.Sprintf("%s/api/v1/label/%s/values", server, name))
			if err != nil {
				return nil, err
			}

			go func(ctx context.Context, retChan chan interface{}) {
				serverResult, err := promclient.GetValuesForLabelName(ctx, parsedUrl.String())
				var ret interface{}
				if err != nil {
					ret = err
				} else {
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
					return nil, err
				}
			}
		}
	}

	if errCount == retCount {
		return nil, fmt.Errorf("Unable to fetch from downstream servers")
	}

	return result.Data, nil
}
