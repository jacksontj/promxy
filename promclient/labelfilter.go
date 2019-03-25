package promclient

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

type LabelFilterVisitor struct {
	ls          model.LabelSet
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

		filteredMatchers, ok := FilterMatchers(l.ls, nodeTyped.LabelMatchers)
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

		filteredMatchers, ok := FilterMatchers(l.ls, nodeTyped.LabelMatchers)
		l.filterMatch = l.filterMatch && ok

		if ok {
			nodeTyped.LabelMatchers = filteredMatchers
		} else {
			return nil, nil
		}
	}

	return l, nil
}

func FilterMatchers(ls model.LabelSet, matchers []*labels.Matcher) ([]*labels.Matcher, bool) {
	filteredMatchers := make([]*labels.Matcher, 0, len(matchers))

	// Look over the matchers passed in, if any exist in our labels, we'll do the matcher, and then strip
	for _, matcher := range matchers {
		if localValue, ok := ls[model.LabelName(matcher.Name)]; ok {
			// If the label exists locally and isn't there, then skip it
			if !matcher.Matches(string(localValue)) {
				return nil, false
			}
		} else {
			filteredMatchers = append(filteredMatchers, matcher)
		}
	}
	return filteredMatchers, true
}
