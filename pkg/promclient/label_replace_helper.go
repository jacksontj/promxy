package promclient

import "github.com/prometheus/prometheus/pkg/labels"

// RewriteMatchers go through each matcher and replace the label if matches
func RewriteMatchers(lr map[string]string, matchers []*labels.Matcher) []*labels.Matcher {
	replacedMatchers := make([]*labels.Matcher, 0, len(matchers))

	// Look over the matchers passed in, if any exist in our labels, we'll do the matcher, and then strip
	for _, matcher := range matchers {
		if repl, ok := lr[matcher.Name]; ok {
			newMatcher := &labels.Matcher{
				Type:  matcher.Type,
				Name:  repl,
				Value: matcher.Value,
			}
			replacedMatchers = append(replacedMatchers, newMatcher)
		} else {
			replacedMatchers = append(replacedMatchers, matcher)
		}
	}

	return replacedMatchers
}
