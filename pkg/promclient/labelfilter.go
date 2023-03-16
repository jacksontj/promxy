package promclient

import (
	"context"
	"sync"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql/parser"
)

// TODO: config
func NewLabelFilterClient(a API) (*LabelFilterClient, error) {
	c := &LabelFilterClient{
		API:            a,
		LabelsToFilter: []string{"__name__", "job", "version"},
	}

	// TODO; background
	if err := c.Sync(context.TODO()); err != nil {
		return nil, err
	}

	return c, nil
}

// LabelFilterClient proxies a client and adds the given labels to all results
type LabelFilterClient struct {
	API

	LabelsToFilter []string // Which labels we want to pull to check

	// TODO: move to a local block (to disk)?? or optionally so?
	// labelFilter is a map of labelName -> labelValue -> nothing (for quick lookups)
	labelFilter map[string]map[string]struct{}
}

func (c *LabelFilterClient) Sync(ctx context.Context) error {
	filter := make(map[string]map[string]struct{})

	for _, label := range c.LabelsToFilter {
		labelFilter := make(map[string]struct{})
		// TODO: warn?
		vals, _, err := c.LabelValues(ctx, label, nil, model.Time(0).Time(), model.Now().Time())
		if err != nil {
			return err
		}
		for _, v := range vals {
			labelFilter[string(v)] = struct{}{}
		}
		filter[label] = labelFilter
	}

	c.labelFilter = filter

	return nil
}

// Query performs a query for the given time.
func (c *LabelFilterClient) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	// Parse out the promql query into expressions etc.
	e, err := parser.ParseExpr(query)
	if err != nil {
		return nil, nil, err
	}

	filterVisitor := NewFilterLabelVisitor(c.labelFilter)
	if _, err := parser.Walk(ctx, filterVisitor, &parser.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil, nil
	}

	return c.API.Query(ctx, query, ts)
}

func NewFilterLabelVisitor(filter map[string]map[string]struct{}) *FilterLabelVisitor {
	return &FilterLabelVisitor{
		labelFilter: filter,
		filterMatch: true,
	}
}

// FilterLabel implements the parser.Visitor interface to filter selectors based on a labelstet
type FilterLabelVisitor struct {
	l           sync.Mutex
	labelFilter map[string]map[string]struct{}
	filterMatch bool
}

// Visit checks if the given node matches the labels in the filter
func (l *FilterLabelVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
		for _, matcher := range nodeTyped.LabelMatchers {
			for labelName, labelFilter := range l.labelFilter {
				if matcher.Name == labelName {
					match := false
					// Check that there is a match somewhere!
					for v := range labelFilter {
						if matcher.Matches(v) {
							match = true
							break
						}
					}
					if !match {
						l.l.Lock()
						l.filterMatch = false
						l.l.Unlock()
						return nil, nil
					}
				}
			}
		}
	case *parser.MatrixSelector:
		// TODO:
	}

	return l, nil
}
