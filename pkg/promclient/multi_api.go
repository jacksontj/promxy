package promclient

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
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

// NewMustMultiAPI returns a MultiAPI
func NewMustMultiAPI(apis []API, antiAffinity model.Time, antiAffinityDynamic bool, metricFunc MultiAPIMetricFunc, requiredCount int, preferMax bool) *MultiAPI {
	a, err := NewMultiAPI(apis, antiAffinity, antiAffinityDynamic, metricFunc, requiredCount, preferMax)
	if err != nil {
		panic(err)
	}
	return a
}

// NewMultiAPI returns a MultiAPI. When antiAffinityDynamic is true,
// MergeSampleStream infers the per-series anti-affinity from the inter-
// sample spacing of the data, using antiAffinity only as a fallback when
// too few samples are present to estimate. This lets a single server-
// group host series with mixed scrape intervals without forcing one
// global buffer that's wrong for half the metrics. See #734.
func NewMultiAPI(apis []API, antiAffinity model.Time, antiAffinityDynamic bool, metricFunc MultiAPIMetricFunc, requiredCount int, preferMax bool) (*MultiAPI, error) {
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
			return nil, fmt.Errorf("unable to create multiAPI as the APIs aren't unique")
		}
	}

	return &MultiAPI{
		apis:                apis,
		apiFingerprints:     apiFingerprints,
		antiAffinity:        antiAffinity,
		antiAffinityDynamic: antiAffinityDynamic,
		metricFunc:          metricFunc,
		requiredCount:       requiredCount,
		preferMax:           preferMax,
	}, nil
}

// MultiAPI implements the API interface while merging the results from the apis it wraps
type MultiAPI struct {
	apis                []API
	apiFingerprints     []model.Fingerprint
	antiAffinity        model.Time
	antiAffinityDynamic bool
	metricFunc          MultiAPIMetricFunc
	requiredCount       int // number "per key" that we require to respond
	preferMax           bool
}

func (m *MultiAPI) recordMetric(i int, api, status string, took float64) {
	if m.metricFunc != nil {
		m.metricFunc(i, api, status, took)
	}
}

// LabelValues performs a query for the values of the given label.
func (m *MultiAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        model.LabelValues
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API, label string) {
			start := time.Now()
			result, w, err := api.LabelValues(childContext, label, matchers, startTime, endTime)
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
		}(i, resultChan, api, label)
	}

	var result []model.LabelValue
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChan:
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
func (m *MultiAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v        []string
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			start := time.Now()
			result, w, err := api.LabelNames(childContext, matchers, startTime, endTime)
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
		}(i, resultChan, api)
	}

	result := make(map[string]struct{})
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChan:
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
// scatterMerge fans `call` out across all members, applies the
// success/requiredCount HA logic, and merges the successful SeriesSets with
// anti-affinity. Warnings are unioned across all responses.
func (m *MultiAPI) scatterMerge(ctx context.Context, op string, call func(context.Context, API) storage.SeriesSet) storage.SeriesSet {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		i  int // index of the API that produced this result
		ss storage.SeriesSet
		ls model.Fingerprint
	}
	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int)

	for i, api := range m.apis {
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			start := time.Now()
			ss := call(childContext, api)
			took := time.Since(start)
			status := "success"
			if ss.Err() != nil {
				status = "error"
			}
			m.recordMetric(i, op, status, took.Seconds())
			retChan <- chanResult{i: i, ss: ss, ls: m.apiFingerprints[i]}
		}(i, resultChan, api)
	}

	// Parked in slots and merged below in API order: mergeAntiAffinity
	// resolves duplicate samples in the order it receives the sets, so the
	// result must not depend on which replica answered first.
	slots := make([]storage.SeriesSet, len(m.apis))
	var warnings annotations.Annotations
	var lastError error
	successMap := make(map[model.Fingerprint]int)
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return promapi.NewSeriesSet(nil, warnings, ctx.Err())

		case ret := <-resultChan:
			outstandingRequests[ret.ls]--
			warnings = MergeAnnotations(warnings, ret.ss.Warnings())
			if err := NormalizePromError(ret.ss.Err()); err != nil {
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return promapi.NewSeriesSet(nil, warnings, err)
				}
				lastError = err
			} else {
				successMap[ret.ls]++
				slots[ret.i] = ret.ss
			}
		}
	}

	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return promapi.NewSeriesSet(nil, warnings, errors.Wrap(lastError, "Unable to fetch from downstream servers"))
		}
	}

	sets := make([]storage.SeriesSet, 0, len(slots))
	for _, ss := range slots {
		if ss != nil {
			sets = append(sets, ss)
		}
	}

	return WithWarnings(MergeSeriesSets(m.antiAffinity, m.antiAffinityDynamic, m.preferMax, sets...), warnings)
}

func (m *MultiAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return m.scatterMerge(ctx, "query", func(c context.Context, api API) storage.SeriesSet {
		return api.Query(c, query, ts)
	})
}

// QueryRange performs a query for the given range.
func (m *MultiAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return m.scatterMerge(ctx, "query_range", func(c context.Context, api API) storage.SeriesSet {
		return api.QueryRange(c, query, r)
	})
}

// Series finds series by label matchers.
func (m *MultiAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		i        int // index of the API that produced this result
		v        []model.LabelSet
		warnings v1.Warnings
		err      error
		ls       model.Fingerprint
	}

	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
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
				i:        i,
				v:        result,
				warnings: w,
				err:      NormalizePromError(err),
				ls:       m.apiFingerprints[i],
			}
		}(i, resultChan, api)
	}

	// Parked in slots and merged below in API order: the result is unsorted
	// and MergeLabelSets keeps the first labelset per fingerprint, so the
	// merge order is observable.
	slots := make([][]model.LabelSet, len(m.apis))
	responded := make([]bool, len(m.apis))
	warnings := make(promhttputil.WarningSet)
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, warnings.Warnings(), ctx.Err()

		case ret := <-resultChan:
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
				slots[ret.i] = ret.v
				responded[ret.i] = true
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, warnings.Warnings(), errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	var result []model.LabelSet
	var haveResult bool
	for i, v := range slots {
		if !responded[i] {
			continue
		}
		if !haveResult {
			result = v
			haveResult = true
		} else {
			result = MergeLabelSets(result, v)
		}
	}

	return result, warnings.Warnings(), nil
}

// GetValue fetches the raw collected data, merged across HA members.
func (m *MultiAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return m.scatterMerge(ctx, "get_value", func(c context.Context, api API) storage.SeriesSet {
		return api.GetValue(c, start, end, matchers)
	})
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (m *MultiAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		i   int // index of the API that produced this result
		v   map[string][]v1.Metadata
		err error
		ls  model.Fingerprint
	}

	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int) // fingerprint -> outstanding

	for i, api := range m.apis {
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API, metric, limit string) {
			start := time.Now()
			result, err := api.Metadata(childContext, metric, limit)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "query", "error", took.Seconds())
			} else {
				m.recordMetric(i, "query", "success", took.Seconds())
			}
			retChan <- chanResult{
				i:   i,
				v:   result,
				err: NormalizePromError(err),
				ls:  m.apiFingerprints[i],
			}
		}(i, resultChan, api, metric, limit)
	}

	// Parked in slots and merged below in API order: the merge is
	// first-writer-wins per metric name, so the merge order decides which
	// downstream's metadata wins.
	slots := make([]map[string][]v1.Metadata, len(m.apis))
	responded := make([]bool, len(m.apis))
	var lastError error
	successMap := make(map[model.Fingerprint]int) // fingerprint -> success
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChan:
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				// If there aren't enough outstanding requests to possibly succeed, no reason to wait
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, ret.err
				}
				lastError = ret.err
			} else {
				successMap[ret.ls]++
				slots[ret.i] = ret.v
				responded[ret.i] = true
			}
		}
	}

	// Verify that we hit the requiredCount for all of the buckets
	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	var result map[string][]v1.Metadata
	var haveResult bool
	for i, v := range slots {
		if !responded[i] {
			continue
		}
		if !haveResult {
			result = v
			haveResult = true
		} else {
			// Merge metadata!
			for k, vv := range v {
				if _, ok := result[k]; !ok {
					result[k] = vv
				}
			}
		}
	}

	return result, nil
}

// exemplarKey is the identity of a single exemplar within a series. Prometheus
// identifies an exemplar by its timestamp, value and labels; a series may
// legitimately carry multiple exemplars at the same timestamp (with different
// trace IDs), so all three are part of the key.
type exemplarKey struct {
	ts     model.Time
	value  model.SampleValue
	labels string
}

func newExemplarKey(e v1.Exemplar) exemplarKey {
	// model.LabelSet.String() sorts label names, so it is a stable key.
	return exemplarKey{ts: e.Timestamp, value: e.Value, labels: e.Labels.String()}
}

// QueryExemplars performs a query for exemplars by the given query and time range.
// We fan the query out to every server-group and merge the per-series exemplar
// lists (set-style union -- the same exemplar returned by multiple downstreams,
// e.g. HA replicas of the same prometheus, is returned only once).
func (m *MultiAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	type chanResult struct {
		v   []v1.ExemplarQueryResult
		err error
		ls  model.Fingerprint
	}

	// Buffered to len(apis): the loop below can return before draining, and a
	// blocked sender would leak its goroutine.
	resultChan := make(chan chanResult, len(m.apis))
	outstandingRequests := make(map[model.Fingerprint]int)

	for i, api := range m.apis {
		outstandingRequests[m.apiFingerprints[i]]++
		go func(i int, retChan chan chanResult, api API) {
			start := time.Now()
			result, err := api.QueryExemplars(childContext, query, startTime, endTime)
			took := time.Since(start)
			if err != nil {
				m.recordMetric(i, "query_exemplars", "error", took.Seconds())
			} else {
				m.recordMetric(i, "query_exemplars", "success", took.Seconds())
			}
			retChan <- chanResult{
				v:   result,
				err: NormalizePromError(err),
				ls:  m.apiFingerprints[i],
			}
		}(i, resultChan, api)
	}

	// Merge by series labels.
	merged := map[model.Fingerprint]*v1.ExemplarQueryResult{}
	// Per-series set of exemplars we've already collected, used to drop the
	// duplicates that the fan-out across HA replicas necessarily returns.
	seen := map[model.Fingerprint]map[exemplarKey]struct{}{}
	var lastError error
	successMap := make(map[model.Fingerprint]int)
	for i := 0; i < len(m.apis); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChan:
			outstandingRequests[ret.ls]--
			if ret.err != nil {
				if (outstandingRequests[ret.ls] + successMap[ret.ls]) < m.requiredCount {
					return nil, ret.err
				}
				lastError = ret.err
				continue
			}
			successMap[ret.ls]++
			for j := range ret.v {
				qr := ret.v[j]
				fp := qr.SeriesLabels.Fingerprint()
				existing, ok := merged[fp]
				if !ok {
					existing = &v1.ExemplarQueryResult{SeriesLabels: qr.SeriesLabels}
					merged[fp] = existing
					seen[fp] = make(map[exemplarKey]struct{}, len(qr.Exemplars))
				}
				seenSeries := seen[fp]
				for _, e := range qr.Exemplars {
					k := newExemplarKey(e)
					if _, dup := seenSeries[k]; dup {
						continue
					}
					seenSeries[k] = struct{}{}
					existing.Exemplars = append(existing.Exemplars, e)
				}
			}
		}
	}

	for k := range outstandingRequests {
		if successMap[k] < m.requiredCount {
			return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
		}
	}

	out := make([]v1.ExemplarQueryResult, 0, len(merged))
	for _, qr := range merged {
		// The downstream API returns exemplars ordered by timestamp; preserve
		// that ordering after merging results from multiple downstreams.
		SortExemplars(qr.Exemplars)
		out = append(out, *qr)
	}
	// Map iteration order is random, so sort by series labels to make repeated
	// identical requests return identical results.
	sort.Slice(out, func(i, j int) bool { return out[i].SeriesLabels.String() < out[j].SeriesLabels.String() })
	return out, nil
}

// SortExemplars orders exemplars by timestamp, then value, then labels. The
// ordering must be total -- over exactly the fields exemplarKey identifies an
// exemplar by -- because downstream responses are merged in completion order,
// so timestamp alone would leave ties resolved by whichever replica was first.
func SortExemplars(exemplars []v1.Exemplar) {
	sort.Slice(exemplars, func(i, j int) bool {
		a, b := exemplars[i], exemplars[j]
		if a.Timestamp != b.Timestamp {
			return a.Timestamp < b.Timestamp
		}
		if a.Value != b.Value {
			return a.Value < b.Value
		}
		return a.Labels.String() < b.Labels.String()
	})
}
