package promclient

import (
	"context"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/sirupsen/logrus"
	"time"
)

// LabelReplaceClient proxies a client and replace labels in the query and result
type LabelReplaceClient struct {
	API
	LabelReplaces map[string]string
}

func (c *LabelReplaceClient) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	matchersToUse, err := c.rewriteMatchers(ctx, matchers)
	if err != nil {
		return nil, nil, err
	}

	l, w, err := c.API.LabelNames(ctx, matchersToUse, startTime, endTime)
	if err != nil {
		return nil, nil, err
	}

	// add the new label to the returned labels, we do not remove the label that is being replaced
	for k, _ := range c.LabelReplaces {
		found := false
		for _, labelName := range l {
			if labelName == k {
				found = true
			}
		}
		if !found {
			l = append(l, k)
		}
	}
	return l, w, err
}

func (c *LabelReplaceClient) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	matchersToUse, err := c.rewriteMatchers(ctx, matchers)
	if err != nil {
		return nil, nil, err
	}

	// TODO: this is rather strange, this replacement code doesn't run, but somehow it still returns the correct data
	labelToUse := label
	if repl, ok := c.LabelReplaces[label]; ok {
		labelToUse = repl
	}

	val, w, err := c.API.LabelValues(ctx, labelToUse, matchersToUse, startTime, endTime)
	if err != nil {
		return nil, w, err
	}

	return val, w, nil
}

// Query performs a query for the given time.
func (c *LabelReplaceClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	queryToUse, err := c.rewriteQuery(ctx, query)
	if err != nil {
		return nil, nil, err
	}

	val, w, err := c.API.Query(ctx, queryToUse, ts)
	if err != nil {
		return nil, w, err
	}
	if err := c.replaceLabelsInMetricValue(val); err != nil {
		return nil, w, err
	}
	return val, w, nil
}

// QueryRange performs a query for the given range.
func (c *LabelReplaceClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	queryToUse, err := c.rewriteQuery(ctx, query)
	if err != nil {
		return nil, nil, err
	}

	val, w, err := c.API.QueryRange(ctx, queryToUse, r)
	if err != nil {
		return nil, w, err
	}
	if err := c.replaceLabelsInMetricValue(val); err != nil {
		return nil, w, err
	}
	return val, w, nil
}

// TODO: promxy's own version of this doesn't work, so this does not work either
func (c *LabelReplaceClient) Series(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	matchersToUse, err := c.rewriteMatchers(ctx, matchers)
	if err != nil {
		return nil, nil, err
	}

	v, w, err := c.API.Series(ctx, matchersToUse, startTime, endTime)
	if err != nil {
		return nil, w, err
	}

	c.replaceLabelsInLabelSet(v)
	return v, w, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *LabelReplaceClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	matchersToUse := RewriteMatchers(c.LabelReplaces, matchers)

	val, w, err := c.API.GetValue(ctx, start, end, matchersToUse)
	if err != nil {
		return nil, w, err
	}

	if err := c.replaceLabelsInMetricValue(val); err != nil {
		return nil, w, err
	}
	return val, w, nil
}

func (c *LabelReplaceClient) rewriteQuery(ctx context.Context, query string) (string, error) {
	logrus.Debugf("Query before label replacement: " + query)
	e, err := parser.ParseExpr(query)
	if err != nil {
		return query, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	replaceVisitor := NewLabelReplaceVisitor(c.LabelReplaces)
	if _, err := parser.Walk(ctx, replaceVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return query, err
	}

	newQuery := e.String()
	logrus.Debugf("Query after label replacement: " + newQuery)
	return newQuery, nil
}

func (c *LabelReplaceClient) rewriteMatchers(ctx context.Context, queries []string) ([]string, error) {
	replacedQueries := make([]string, 0, len(queries))
	for _, query := range queries {
		repl, err := c.rewriteQuery(ctx, query)
		if err != nil {
			return queries, err
		}
		replacedQueries = append(replacedQueries, repl)
	}
	return replacedQueries, nil
}

func (c *LabelReplaceClient) replaceLabelsInMetricValue(a model.Value) error {
	switch aTyped := a.(type) {
	case model.Vector:
		for _, item := range aTyped {
			for k, v := range c.LabelReplaces {
				if _, ok := item.Metric[model.LabelName(v)]; ok {
					item.Metric[model.LabelName(k)] = item.Metric[model.LabelName(v)]
				}
			}
		}

	case model.Matrix:
		for _, item := range aTyped {
			// If the current metric has no labels, set them
			if item.Metric == nil {
				item.Metric = model.Metric(model.LabelSet(make(map[model.LabelName]model.LabelValue)))
			}
			for k, v := range c.LabelReplaces {
				if _, ok := item.Metric[model.LabelName(v)]; ok {
					item.Metric[model.LabelName(k)] = item.Metric[model.LabelName(v)]
				}
			}
		}
	}

	return nil
}

func (c *LabelReplaceClient) replaceLabelsInLabelSet(ls []model.LabelSet) {
	for _, item := range ls {
		for k, v := range c.LabelReplaces {
			if _, ok := item[model.LabelName(v)]; ok {
				item[model.LabelName(k)] = item[model.LabelName(v)]
			}
		}
	}
}
