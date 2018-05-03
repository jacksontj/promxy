package servergroup

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

type LabelFilterVisitor struct {
	s           *ServerGroup
	filterMatch bool
}

func (l *LabelFilterVisitor) Visit(node promql.Node, path []promql.Node) (w promql.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *promql.VectorSelector:
		for _, matcher := range nodeTyped.LabelMatchers {
			if matcher.Name == model.MetricNameLabel && matcher.Type == labels.MatchEqual {
				nodeTyped.Name = matcher.Value
			}
		}

		filteredMatchers, ok := l.s.FilterMatchers(nodeTyped.LabelMatchers)
		l.filterMatch = l.filterMatch && ok

		if ok {
			nodeTyped.LabelMatchers = filteredMatchers
		} else {
			return nil, nil
		}
	case *promql.MatrixSelector:
		for _, matcher := range nodeTyped.LabelMatchers {
			if matcher.Name == model.MetricNameLabel && matcher.Type == labels.MatchEqual {
				nodeTyped.Name = matcher.Value
			}
		}

		filteredMatchers, ok := l.s.FilterMatchers(nodeTyped.LabelMatchers)
		l.filterMatch = l.filterMatch && ok

		if ok {
			nodeTyped.LabelMatchers = filteredMatchers
		} else {
			return nil, nil
		}
	}

	return l, nil
}
