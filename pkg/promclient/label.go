package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// MergeLabelValues merges the labels from b into a
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

// MergeLabelSets merges the labelset b into a
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

// Key defines the labelset which identifies this client
func (c *AddLabelClient) Key() model.LabelSet {
	return c.Labels
}

func (c *AddLabelClient) filterMatchers(matchers []string) ([]string, bool, error) {
	ret := make([]string, 0, len(matchers))
	for _, matcher := range matchers {
		selectors, err := parser.ParseMetricSelector(matcher)
		if err != nil {
			return nil, true, err
		}

		filteredSelectors := make([]*labels.Matcher, 0, len(selectors))

		// If the selector matches our value -- remove the selector
		// if the selector doesn't match, return empty
		for _, s := range selectors {
			if v, ok := c.Labels[model.LabelName(s.Name)]; ok {
				// If the selector doesn't match the labels from our client; we don't match
				if !s.Matches(string(v)) {
					return nil, false, nil
				}
			} else { // Otherwise if the selector isn't part of the `Labels` we add; we pass it along
				filteredSelectors = append(filteredSelectors, s)
			}
		}
		// If the selector is cleared -- then we skip it in the return
		if len(filteredSelectors) == 0 {
			continue
		}
		newMatcher, err := promhttputil.MatcherToString(filteredSelectors)
		if err != nil {
			return nil, false, err
		}
		ret = append(ret, newMatcher)
	}
	return ret, true, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (c *AddLabelClient) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	matchers, ok, err := c.filterMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, nil
	}

	l, w, err := c.API.LabelNames(ctx, matchers, startTime, endTime)
	if err != nil {
		return nil, nil, err
	}

	for k := range c.Labels {
		found := false
		for _, labelName := range l {
			if labelName == string(k) {
				found = true
			}
		}
		if !found {
			l = append(l, string(k))
		}
	}

	return l, w, err
}

// LabelValues performs a query for the values of the given label.
func (c *AddLabelClient) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	matchers, ok, err := c.filterMatchers(matchers)
	if err != nil {
		return nil, nil, err
	}
	if !ok {
		return nil, nil, nil
	}

	val, w, err := c.API.LabelValues(ctx, label, matchers, startTime, endTime)
	if err != nil {
		return nil, w, err
	}

	// do we have labels that match in our state
	if value, ok := c.Labels[model.LabelName(label)]; ok {
		return MergeLabelValues(val, model.LabelValues{value}), w, nil
	}
	return val, w, nil
}

// Query performs a query for the given time.
func (c *AddLabelClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return nil, nil, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	filterVisitor := NewFilterMatcherVisitor(c.Labels)
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil, nil
	}

	val, w, err := c.API.Query(ctx, e.String(), ts)
	if err != nil {
		return nil, w, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, w, err
	}
	return val, w, nil
}

// QueryRange performs a query for the given range.
func (c *AddLabelClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return nil, nil, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	filterVisitor := NewFilterMatcherVisitor(c.Labels)
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil, nil
	}

	val, w, err := c.API.QueryRange(ctx, e.String(), r)
	if err != nil {
		return nil, w, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, w, err
	}
	return val, w, nil
}

// Series finds series by label matchers.
func (c *AddLabelClient) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	// Now we need to filter the matches sent to us for the labels associated with this
	// servergroup
	filteredMatches := make([]string, 0, len(matches))
	for _, matcher := range matches {
		// Parse out the promql query into expressions etc.
		e, err := parser.ParseExpr(matcher)
		if err != nil {
			return nil, nil, err
		}

		// Walk the expression, to filter out any LabelMatchers that match etc.
		filterVisitor := NewFilterMatcherVisitor(c.Labels)
		if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
			return nil, nil, err
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
		return nil, nil, nil
	}

	v, w, err := c.API.Series(ctx, filteredMatches, startTime, endTime)
	if err != nil {
		return nil, w, err
	}

	// add our state's labels to the labelsets we return
	for _, lset := range v {
		for k, v := range c.Labels {
			lset[k] = v
		}
	}

	return v, w, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *AddLabelClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	filteredMatchers, ok := FilterMatchers(c.Labels, matchers)
	if !ok {
		return nil, nil, nil
	}

	val, w, err := c.API.GetValue(ctx, start, end, filteredMatchers)
	if err != nil {
		return nil, w, err
	}
	if err := promhttputil.ValueAddLabelSet(val, c.Labels); err != nil {
		return nil, w, err
	}

	return val, w, nil
}
