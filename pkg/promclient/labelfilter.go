package promclient

import (
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

// LabelFilterVisitor implements the parser.Visitor interface to filter selectors based on a labelstet
type LabelFilterVisitor struct {
	ls          model.LabelSet
	filterMatch bool
}

// Visit checks if the given node matches the labels in the filter
func (l *LabelFilterVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
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
	case *parser.MatrixSelector:
		for _, matcher := range nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers {
			if matcher.Name == model.MetricNameLabel && matcher.Type == labels.MatchEqual {
				nodeTyped.VectorSelector.(*parser.VectorSelector).Name = matcher.Value
			}
		}

		filteredMatchers, ok := FilterMatchers(l.ls, nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers)
		l.filterMatch = l.filterMatch && ok

		if ok {
			nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers = filteredMatchers
		} else {
			return nil, nil
		}
	}

	return l, nil
}

// FilterMatchers applies the matchers to the given labelset to determine if there is a match
// and to return all remaining matchers to be matched
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

	// Prometheus doesn't support empty matchers (https://github.com/prometheus/prometheus/issues/2162)
	// so if we filter out all matchers we want to replace the empty matcher
	// with a matcher that does the same
	if len(filteredMatchers) == 0 {
		filteredMatchers = append(filteredMatchers, &labels.Matcher{
			Type:  labels.MatchRegexp,
			Name:  labels.MetricName,
			Value: ".+",
		})
	}

	return filteredMatchers, true
}
