package alertbackfill

import (
	"context"
	"errors"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/rules"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/proxyquerier"
)

// RuleGroupFetcher defines a method to fetch []*rules.Group
type RuleGroupFetcher func() []*rules.Group

// NewAlertBackfillQueryable returns a new AlertBackfillQueryable
func NewAlertBackfillQueryable(e *promql.Engine, q storage.Queryable) *AlertBackfillQueryable {
	return &AlertBackfillQueryable{e: e, q: q}
}

// AlertBackfillQueryable returns a storage.Queryable that will handle returning
// results for the RuleManager alert backfill. This is done by first attempting
// to query the downstream store and if no result is found it will "recreate" the
// the series by re-running the necessary query to get the data back
type AlertBackfillQueryable struct {
	e *promql.Engine
	q storage.Queryable
	f RuleGroupFetcher
}

// SetRuleGroupFetcher sets the RuleGroupFetcher -- this is required as we need
// the groups to find the correct step interval
func (q *AlertBackfillQueryable) SetRuleGroupFetcher(f RuleGroupFetcher) {
	q.f = f
}

// Querier returns an AlertBackfillQuerier
func (q *AlertBackfillQueryable) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	api, err := promclient.NewEngineAPI(q.e, q.q)
	if err != nil {
		return nil, err
	}

	return &AlertBackfillQuerier{
		ctx:        ctx,
		api:        api,
		q:          q.q,
		ruleGroups: q.f(),
		ruleValues: make(map[string]*queryResult),
		mint:       mint,
		maxt:       maxt,
	}, nil
}

type queryResult struct {
	v        model.Value
	warnings storage.Warnings
	err      error
}

// TODO: move to a util package?
func StringsToWarnings(ins []string) storage.Warnings {
	warnings := make(storage.Warnings, len(ins))
	for i, in := range ins {
		warnings[i] = errors.New(in)
	}

	return warnings
}

// AlertBackfillQuerier will Query a downstream storage.Queryable for the
// ALERTS_FOR_STATE series, if that series is not found -- it will then
// run the appropriate query_range equivalent to re-generate the data.
type AlertBackfillQuerier struct {
	ctx        context.Context
	q          storage.Queryable
	api        promclient.API
	ruleGroups []*rules.Group

	// map of (groupidx.ruleidx) -> result
	ruleValues map[string]*queryResult
	mint       int64
	maxt       int64
}

// Select will fetch and return the ALERTS_FOR_STATE series for the given matchers
func (q *AlertBackfillQuerier) Select(sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	// first, we call the actual downstream to see if we have the correct data
	// this will return something if the remote_write from promxy has been saved
	// somewhere where promxy is also configured to read from
	querier, err := q.q.Querier(q.ctx, q.mint, q.maxt)
	if err != nil {
		return proxyquerier.NewSeriesSet(nil, nil, err)
	}
	ret := querier.Select(sortSeries, hints, matchers...)
	downstreamSeries := make([]storage.Series, 0)
	for ret.Next() {
		downstreamSeries = append(downstreamSeries, ret.At())
	}
	// If the raw queryable had something; return that
	if len(downstreamSeries) > 0 {
		return proxyquerier.NewSeriesSet(downstreamSeries, ret.Warnings(), ret.Err())
	}

	// Find our Rule + interval
	key, matchingRule, interval := FindGroupAndAlert(q.ruleGroups, matchers)

	// If we can't find a matching rule; return an empty set
	if matchingRule == nil {
		return proxyquerier.NewSeriesSet(nil, nil, nil)
	}

	result, ok := q.ruleValues[key]
	// If we haven't queried this *rule* before; lets load that
	if !ok {
		now := time.Now()
		value, warnings, err := q.api.QueryRange(q.ctx, matchingRule.Query().String(), v1.Range{
			// Start is the HoldDuration + 1 step (to avoid "edge" issues)
			Start: now.Add(-1 * matchingRule.HoldDuration()).Add(-1 * interval),
			End:   now,
			Step:  interval,
		})
		result = &queryResult{
			v:        value,
			warnings: StringsToWarnings(warnings),
			err:      err,
		}
		q.ruleValues[key] = result
	}

	// If there are any errors; we can return those
	if result.err != nil {
		return proxyquerier.NewSeriesSet(nil, result.warnings, result.err)
	}

	resultMatrix, ok := result.v.(model.Matrix)
	if !ok {
		return proxyquerier.NewSeriesSet(nil, nil, fmt.Errorf("backfill query returned unexpected type: %T", result.v))
	}

	iterators := promclient.IteratorsForValue(GenerateAlertStateMatrix(resultMatrix, matchers, matchingRule.Labels(), interval))

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &proxyquerier.Series{iterator}
	}

	return proxyquerier.NewSeriesSet(series, result.warnings, nil)
}

func (q *AlertBackfillQuerier) LabelValues(name string, matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func (q *AlertBackfillQuerier) LabelNames(matchers ...*labels.Matcher) ([]string, storage.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

func (q *AlertBackfillQuerier) Close() error { return nil }
