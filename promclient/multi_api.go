package promclient

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/jacksontj/promxy/promhttputil"
)

// Since these error types magically add in their own prefixes, we need to get
// the prefix so it doesn't get added twice
var (
	timeoutPrefix  = promql.ErrQueryTimeout("").Error()
	canceledPrefix = promql.ErrQueryCanceled("").Error()
)

// NormalizePromError converts the errors that the prometheus API client returns
// into errors that the prometheus API server actually handles and returns proper
// error codes for
func NormalizePromError(err error) error {
	if typedErr, ok := err.(*v1.Error); ok {
		res := &DataResult{}
		// The prometheus client does a terrible job of handling and returning errors
		// so we need to do the work ourselves.
		// The `Detail` is actually just the body of the response so we need
		// to unmarshal that so we can see what happened
		if err := res.UnmarshalJSON([]byte(typedErr.Detail)); err != nil {
			// If the body can't be unmarshaled, return the original error
			return typedErr
		}

		// Now we want to switch for any errors that the API server will handle differently
		switch res.ErrorType {
		case promhttputil.ErrorTimeout:
			return promql.ErrQueryTimeout(strings.TrimPrefix(res.Error, timeoutPrefix))
		case promhttputil.ErrorCanceled:
			return promql.ErrQueryCanceled(strings.TrimPrefix(res.Error, canceledPrefix))
		}
	}

	// If all else fails, return the original error
	return err
}

// MultiAPIMetricFunc defines a method where a client can record metrics about
// the specific API calls made through this multi client
type MultiAPIMetricFunc func(i int, api, status string, took float64)

// NewMultiAPI returns a MultiAPI
func NewMultiAPI(apis []API, antiAffinity model.Time, metricFunc MultiAPIMetricFunc) *MultiAPI {
	return &MultiAPI{
		apis:         apis,
		antiAffinity: antiAffinity,
		metricFunc:   metricFunc,
	}
}

// MultiAPI implements the API interface while merging the results from the apis it wraps
type MultiAPI struct {
	apis         []API
	antiAffinity model.Time
	metricFunc   MultiAPIMetricFunc
}

func (m *MultiAPI) recordMetric(i int, api, status string, took float64) {
	if m.metricFunc != nil {
		m.metricFunc(i, api, status, took)
	}
}

// LabelValues performs a query for the values of the given label.
func (m *MultiAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API, label string) {
			start := time.Now()
			result, err := api.LabelValues(childContext, label)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "label_values", "error", took.Seconds())
				retChan <- NormalizePromError(err)
			} else {
				m.recordMetric(i, "label_values", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api, label)
	}

	// Wait for results as we get them
	var result []model.LabelValue
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.LabelValues:
				if result == nil {
					result = retTyped
				} else {
					result = MergeLabelValues(result, retTyped)
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

// Query performs a query for the given time.
func (m *MultiAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	fetchers := make([]Fetcher, len(m.apis))

	// Define getFetcher before the loop, otherwise we end up fetching everything from one
	getFetcher := func(api API) Fetcher {
		return FetcherFunc(func(ctx context.Context) (model.Value, error) {
			return api.Query(ctx, query, ts)
		})
	}
	for i, api := range m.apis {
		fetchers[i] = getFetcher(api)
	}

	v, err := MultiFetch(ctx, m.antiAffinity, fetchers...)
	if err != nil {
		return nil, err
	}
	return v, nil
}

// QueryRange performs a query for the given range.
func (m *MultiAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	fetchers := make([]Fetcher, len(m.apis))

	// Define getFetcher before the loop, otherwise we end up fetching everything from one
	getFetcher := func(api API) Fetcher {
		return FetcherFunc(func(ctx context.Context) (model.Value, error) {
			return api.QueryRange(ctx, query, r)
		})
	}
	for i, api := range m.apis {
		fetchers[i] = getFetcher(api)
	}

	v, err := MultiFetch(ctx, m.antiAffinity, fetchers...)
	if err != nil {
		return nil, err
	}

	return v, nil
}

// Series finds series by label matchers.
func (m *MultiAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(m.apis))

	for i, api := range m.apis {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, api API) {
			start := time.Now()
			result, err := api.Series(childContext, matches, startTime, endTime)
			took := time.Now().Sub(start)
			if err != nil {
				m.recordMetric(i, "series", "error", took.Seconds())
				retChan <- NormalizePromError(err)
			} else {
				m.recordMetric(i, "series", "success", took.Seconds())
				retChan <- result
			}
		}(i, resultChans[i], api)
	}

	// Wait for results as we get them
	var result []model.LabelSet
	var lastError error
	errCount := 0
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case []model.LabelSet:
				if result == nil {
					result = retTyped
				} else {
					result = MergeLabelSets(result, retTyped)
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(m.apis) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

// GetValue fetches a `model.Value` which represents the actual collected data
func (m *MultiAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	fetchers := make([]Fetcher, len(m.apis))

	// Define getFetcher before the loop, otherwise we end up fetching everything from one
	getFetcher := func(api API) Fetcher {
		return FetcherFunc(func(ctx context.Context) (model.Value, error) {
			return api.GetValue(ctx, start, end, matchers)
		})
	}
	for i, api := range m.apis {
		fetchers[i] = getFetcher(api)
	}

	v, err := MultiFetch(ctx, m.antiAffinity, fetchers...)
	if err != nil {
		return nil, err
	}
	return v, nil
}
