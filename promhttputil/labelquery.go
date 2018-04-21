package promhttputil

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

// convert list of labelMatchers to a promql string
// MatcherToString converts a []*labels.Matcher into the actual matcher you would
// see on the wire (such as `metricname{label="value"}`)
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
