package promclient

import (
	"sync"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

func NewFilterMatcherVisitor(ls, extLs model.LabelSet) *FilterMatcherVisitor {
	return &FilterMatcherVisitor{
		ls:          ls,
		extLs:       extLs,
		filterMatch: true,
	}
}

// FilterMatcherVisitor implements the parser.Visitor interface to filter matchers based on a labelstet
type FilterMatcherVisitor struct {
	l           sync.Mutex
	ls          model.LabelSet
	extLs       model.LabelSet
	filterMatch bool
}

// Visit checks if the given node matches the labels in the filter
func (l *FilterMatcherVisitor) Visit(node parser.Node, path []parser.Node) (w parser.Visitor, err error) {
	switch nodeTyped := node.(type) {
	case *parser.VectorSelector:
		filteredMatchers, ok := FilterMatchers(l.ls, l.extLs, nodeTyped.LabelMatchers)
		l.l.Lock()
		l.filterMatch = l.filterMatch && ok
		l.l.Unlock()

		if ok {
			nodeTyped.LabelMatchers = filteredMatchers
		} else {
			return nil, nil
		}
	case *parser.MatrixSelector:
		filteredMatchers, ok := FilterMatchers(l.ls, l.extLs, nodeTyped.VectorSelector.(*parser.VectorSelector).LabelMatchers)
		l.l.Lock()
		l.filterMatch = l.filterMatch && ok
		l.l.Unlock()

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
func FilterMatchers(ls, extLs model.LabelSet, matchers []*labels.Matcher) ([]*labels.Matcher, bool) {
	filteredMatchers := make([]*labels.Matcher, 0, len(matchers))

	// Look over the matchers passed in, if any exist in our labels, we'll do the matcher, and then strip
	for _, matcher := range matchers {
		if localValue, ok := ls[model.LabelName(matcher.Name)]; ok {
			// If the label exists locally and isn't there, then skip it
			if !matcher.Matches(string(localValue)) {
				return nil, false
			}
		} else if v, ok := extLs[model.LabelName(matcher.Name)]; ok && matcher.Matches(string(v)) {
			continue // If the selector matches the external labels, we skip it
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
