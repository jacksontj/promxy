package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"

	"github.com/jacksontj/promxy/promhttputil"
)

func MergeLabelValues(a, b []model.LabelValue) []model.LabelValue {
	labels := make(map[model.LabelValue]struct{})
	for _, item := range a {
		labels[item] = struct{}{}
	}

	for _, item := range b {
		if _, ok := labels[item]; !ok {
			a = append(a, item)
			labels[item] = struct{}{}
		}
	}
	return a
}

func MergeLabelSets(a, b []model.LabelSet) []model.LabelSet {
	added := make(map[model.Fingerprint]struct{})
	for _, item := range a {
		added[item.Fingerprint()] = struct{}{}
	}

	for _, item := range b {
		fp := item.Fingerprint()
		if _, ok := added[fp]; !ok {
			added[fp] = struct{}{}
			a = append(a, item)
		}
	}

	return a
}

// AddLabelClient proxies a client and adds the given labels to all results
type AddLabelClient struct {
	API
	Labels model.LabelSet
}

// LabelValues performs a query for the values of the given label.
func (c *AddLabelClient) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	val, err := c.API.LabelValues(ctx, label)
	if err != nil {
		return nil, err
	}

	// do we have labels that match in our state
	if value, ok := c.Labels[model.LabelName(label)]; ok {
		return MergeLabelValues(val, model.LabelValues{value}), nil
	}
	return val, nil
}

// Query performs a query for the given time.
func (c *AddLabelClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	// Parse out the promql query into expressions etc.
	e, err := promql.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	filterVisitor := &LabelFilterVisitor{c.Labels, true}
	if _, err := promql.Walk(ctx, filterVisitor, &promql.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil
	}

	val, err := c.API.Query(ctx, e.String(), ts)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, err
	}
	return val, nil
}

// QueryRange performs a query for the given range.
func (c *AddLabelClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	// Parse out the promql query into expressions etc.
	e, err := promql.ParseExpr(query)
	if err != nil {
		return nil, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	filterVisitor := &LabelFilterVisitor{c.Labels, true}
	if _, err := promql.Walk(ctx, filterVisitor, &promql.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil
	}

	val, err := c.API.QueryRange(ctx, e.String(), r)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, err
	}
	return val, nil
}

// Series finds series by label matchers.
func (c *AddLabelClient) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	// Now we need to filter the matches sent to us for the labels associated with this
	// servergroup
	filteredMatches := make([]string, 0, len(matches))
	for _, matcher := range matches {
		// Parse out the promql query into expressions etc.
		e, err := promql.ParseExpr(matcher)
		if err != nil {
			return nil, err
		}

		// Walk the expression, to filter out any LabelMatchers that match etc.
		filterVisitor := &LabelFilterVisitor{c.Labels, true}
		if _, err := promql.Walk(ctx, filterVisitor, &promql.EvalStmt{Expr: e}, e, nil, nil); err != nil {
			return nil, err
		}
		// If we didn't match, lets skip
		if !filterVisitor.filterMatch {
			continue
		}
		// if we did match, lets assign the filtered version of the matcher
		filteredMatches = append(filteredMatches, e.String())
	}

	// If no matchers remain, then we don't have anything -- so skip
	if len(filteredMatches) == 0 {
		return nil, nil
	}

	v, err := c.API.Series(ctx, filteredMatches, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// add our state's labels to the labelsets we return
	for _, lset := range v {
		for k, v := range c.Labels {
			lset[k] = v
		}
	}

	return v, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *AddLabelClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	filteredMatchers, ok := FilterMatchers(c.Labels, matchers)
	if !ok {
		return nil, nil
	}

	val, err := c.API.GetValue(ctx, start, end, filteredMatchers)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, err
	}

	return val, nil
}
