package promhttputil

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

// convert list of labelMatchers to a promql string

func MatcherToString(matchers []*labels.Matcher) (string, error) {
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
