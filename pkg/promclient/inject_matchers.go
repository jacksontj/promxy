package promclient

import (
	"context"
	"sync"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// InjectMatchersClient wraps an API and injects a static set of label matchers into
// every selector of every request sent downstream. This effectively scopes the
// downstream to the subset of data matching those matchers -- even for queries that
// never reference the labels being injected (e.g. `count(up)` becomes
// `count(up{cluster="A"})`).
//
// This differs from the other label mechanisms in promxy:
//   - `labels` only *adds* labels to the responses coming back from the downstream
//   - `label_filter` only *drops* queries whose matchers can't match the downstream
//   - `inject_matchers` always *adds* the configured matchers to the queries themselves
//
// NOTE: this is not a "secure" mechanism. It only mutates the matchers in the request
// and does not (and cannot) prevent a downstream from returning data outside of the
// injected scope if the downstream ignores the matchers. As with `label_filter`, a way
// to use this more securely is to pair it with a trusted label proxy.
type InjectMatchersClient struct {
	API
	matchers []*labels.Matcher
}

// NewInjectMatchersClient returns an InjectMatchersClient that injects the given
// matchers into all requests sent to the downstream API.
func NewInjectMatchersClient(a API, matchers []*labels.Matcher) (*InjectMatchersClient, error) {
	return &InjectMatchersClient{API: a, matchers: matchers}, nil
}

// injectIntoQuery parses the given promql query, injects the configured matchers into
// every selector and returns the rewritten query string.
func (c *InjectMatchersClient) injectIntoQuery(ctx context.Context, query string) (string, error) {
	e, err := parser.ParseExpr(query)
	if err != nil {
		return "", err
	}

	visitor := &injectMatchersVisitor{matchers: c.matchers}
	if _, err := parser.Walk(ctx, visitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return "", err
	}

	return e.String(), nil
}

// injectIntoMatch injects the configured matchers into a single match[] selector
// string (e.g. `up{job="foo"}`) and returns the rewritten selector.
func (c *InjectMatchersClient) injectIntoMatch(match string) (string, error) {
	selectors, err := parser.ParseMetricSelector(match)
	if err != nil {
		return "", err
	}
	return promhttputil.MatcherToString(appendMatchers(selectors, c.matchers))
}

// injectIntoMatches injects the configured matchers into every match[] selector. When
// no matchers are supplied we add a single selector consisting solely of the injected
// matchers so the downstream is still scoped (rather than returning everything).
func (c *InjectMatchersClient) injectIntoMatches(matches []string) ([]string, error) {
	if len(matches) == 0 {
		match, err := promhttputil.MatcherToString(c.matchers)
		if err != nil {
			return nil, err
		}
		return []string{match}, nil
	}

	ret := make([]string, 0, len(matches))
	for _, match := range matches {
		injected, err := c.injectIntoMatch(match)
		if err != nil {
			return nil, err
		}
		ret = append(ret, injected)
	}
	return ret, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (c *InjectMatchersClient) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	matchers, err := c.injectIntoMatches(matchers)
	if err != nil {
		return nil, nil, err
	}
	return c.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (c *InjectMatchersClient) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	matchers, err := c.injectIntoMatches(matchers)
	if err != nil {
		return nil, nil, err
	}
	return c.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (c *InjectMatchersClient) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	query, err := c.injectIntoQuery(ctx, query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return c.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (c *InjectMatchersClient) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	query, err := c.injectIntoQuery(ctx, query)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return c.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (c *InjectMatchersClient) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	matches, err := c.injectIntoMatches(matches)
	if err != nil {
		return nil, nil, err
	}
	return c.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (c *InjectMatchersClient) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return c.API.GetValue(ctx, start, end, appendMatchers(matchers, c.matchers))
}

// injectMatchersVisitor implements the parser.Visitor interface to inject a static set
// of matchers into every selector of a promql expression.
type injectMatchersVisitor struct {
	l        sync.Mutex
	matchers []*labels.Matcher
}

// Visit injects the configured matchers into every VectorSelector. The parser descends
// into the VectorSelector contained in a MatrixSelector, so handling VectorSelector is
// sufficient to cover both instant and range selectors.
func (v *injectMatchersVisitor) Visit(node parser.Node, path []parser.Node) (parser.Visitor, error) {
	if nodeTyped, ok := node.(*parser.VectorSelector); ok {
		v.l.Lock()
		nodeTyped.LabelMatchers = appendMatchers(nodeTyped.LabelMatchers, v.matchers)
		v.l.Unlock()
	}
	return v, nil
}

// appendMatchers returns matchers with toAdd appended, skipping any matcher that is
// already present (same type, name and value) to keep the rewritten selector clean and
// the operation idempotent.
func appendMatchers(matchers []*labels.Matcher, toAdd []*labels.Matcher) []*labels.Matcher {
	ret := make([]*labels.Matcher, len(matchers), len(matchers)+len(toAdd))
	copy(ret, matchers)

	for _, m := range toAdd {
		exists := false
		for _, existing := range ret {
			if existing.Type == m.Type && existing.Name == m.Name && existing.Value == m.Value {
				exists = true
				break
			}
		}
		if !exists {
			ret = append(ret, m)
		}
	}
	return ret
}
