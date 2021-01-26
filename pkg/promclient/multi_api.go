package promclient

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/jacksontj/promxy/pkg/promhttputil"
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
	type result struct {
		ErrorType promhttputil.ErrorType `json:"errorType,omitempty"`
		Error     string                 `json:"error,omitempty"`
	}

	if typedErr, ok := err.(*v1.Error); ok {
		res := &result{}
		// The prometheus client does a terrible job of handling and returning errors
		// so we need to do the work ourselves.
		// The `Detail` is actually just the body of the response so we need
		// to unmarshal that so we can see what happened
		if err := json.Unmarshal([]byte(typedErr.Detail), res); err != nil {
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
func NewMultiAPI(apis []API, antiAffinity model.Time, metricFunc MultiAPIMetricFunc, requiredCount int) *MultiAPI {
	fingerprintCounts := make(map[model.Fingerprint]int)
	apiFingerprints := make([]model.Fingerprint, len(apis))
	for i, api := range apis {
		var fingerprint model.Fingerprint
		if apiLabels, ok := api.(APILabels); ok {
			if keys := apiLabels.Key(); keys != nil {
				fingerprint = keys.FastFingerprint()
			}
		}
		apiFingerprints[i] = fingerprint
		fingerprintCounts[fingerprint]++
	}

	for _, v := range fingerprintCounts {
		if v < requiredCount {
			// TODO: return an error
			panic("not possible")
		}
	}

	return &MultiAPI{
		apis:            apis,
		apiFingerprints: apiFingerprints,
		antiAffinity:    antiAffinity,
		metricFunc:      metricFunc,
		requiredCount:   requiredCount,
	}
}

// MultiAPI implements the API interface while merging the results from the apis it wraps
type MultiAPI struct {
	apis            []API
	apiFingerprints []model.Fingerprint
	antiAffinity    model.Time
	metricFunc      MultiAPIMetricFunc
	requiredCount   int // number "per key" that we require to respond
}

func (m *MultiAPI) recordMetric(i int, api, status string, took float64) {
	if m.metricFunc != nil {
		m.metricFunc(i, api, status, took)
	}
}

// LabelValues performs a query for the values of the given label.
func (m *MultiAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        model.LabelValues
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API, label string) {
			start := time.Now()
			result, w, err := api.LabelValues(childContext, label)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "label_values", "error", took.Seconds())
			} else {
				m.recordMetric(i, "label_values", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api, label)
	}

	// Wait for results as we get them
	var result []model.LabelValue
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				if result == nil {
					result = ret.v
				} else {
					result = MergeLabelValues(result, ret.v)
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	sort.Sort(model.LabelValues(result))

	return result, warnings.Warnings(), nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (m *MultiAPI) LabelNames(ctx context.Context) ([]string, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        []string
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			start := time.Now()
			result, w, err := api.LabelNames(childContext)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "label_names", "error", took.Seconds())
			} else {
				m.recordMetric(i, "label_names", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api)
	}

	// Wait for results as we get them
	result := make(map[string]struct{})
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				for _, v := range ret.v {
					result[v] = struct{}{}
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	stringResult := make([]string, 0, len(result))
	for k := range result {
		stringResult = append(stringResult, k)
	}

	sort.Strings(stringResult)

	return stringResult, warnings.Warnings(), nil
}

// Query performs a query for the given time.
func (m *MultiAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        model.Value
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API, query string, ts time.Time) {
			start := time.Now()
			result, w, err := api.Query(childContext, query, ts)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "query", "error", took.Seconds())
			} else {
				m.recordMetric(i, "query", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api, query, ts)
	}

	// Wait for results as we get them
	var result model.Value
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				if result == nil {
					result = ret.v
				} else {
					var err error
					result, err = promhttputil.MergeValues(m.antiAffinity, result, ret.v)
					if err != nil {
						return nil, warnings.Warnings(), err
					}
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	return result, warnings.Warnings(), nil
}

// QueryRange performs a query for the given range.
func (m *MultiAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        model.Value
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API, query string, r v1.Range) {
			start := time.Now()
			result, w, err := api.QueryRange(childContext, query, r)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "query_range", "error", took.Seconds())
			} else {
				m.recordMetric(i, "query_range", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api, query, r)
	}

	// Wait for results as we get them
	var result model.Value
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				if result == nil {
					result = ret.v
				} else {
					var err error
					result, err = promhttputil.MergeValues(m.antiAffinity, result, ret.v)
					if err != nil {
						return nil, warnings.Warnings(), err
					}
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	return result, warnings.Warnings(), nil
}

// Series finds series by label matchers.
func (m *MultiAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        []model.LabelSet
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			start := time.Now()
			result, w, err := api.Series(childContext, matches, startTime, endTime)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "series", "error", took.Seconds())
			} else {
				m.recordMetric(i, "series", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api)
	}

	// Wait for results as we get them
	var result []model.LabelSet
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				if result == nil {
					result = ret.v
				} else {
					result = MergeLabelSets(result, ret.v)
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	return result, warnings.Warnings(), nil
}

// GetValue fetches a `model.Value` which represents the actual collected data
func (m *MultiAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        model.Value
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	resultChans := make([]chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	// Scatter out all the queries
	for i, api := range m.apis {
		resultChans[i] = make(chan chanResult, 1)
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			queryStart := time.Now()
			result, w, err := api.GetValue(childContext, start, end, matchers)
			took := time.Since(queryStart)
			if err != nil {
				m.recordMetric(i, "get_value", "error", took.Seconds())
			} else {
				m.recordMetric(i, "get_value", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChans[i], api)
	}

	// Wait for results as we get them
	var result model.Value
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChans[i]:
			warnings.AddWarnings(ret.warnings)
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, warnings.Warnings(), ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				if result == nil {
					result = ret.v
				} else {
					var err error
					result, err = promhttputil.MergeValues(m.antiAffinity, result, ret.v)
					if err != nil {
						return nil, warnings.Warnings(), err
					}
				}
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	return result, warnings.Warnings(), nil
}
