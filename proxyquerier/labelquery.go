package proxyquerier

import (
	"github.com/prometheus/prometheus/storage/metric"
)

// convert list of labelMatchers to a promql string

func MatcherToString(matchers []*metric.LabelMatcher) (string, error) {
	ret := "{"
	for i, matcher := range matchers {
		if i > 0 {
			ret += ","
		}
		ret += matcher.String()
	}
	ret += "}"

	return ret, nil
}
