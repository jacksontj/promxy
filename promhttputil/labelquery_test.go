package promhttputil

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
)

func TestMatcherToString(t *testing.T) {
	tests := []struct {
		matchers []*labels.Matcher
		result   string
		err      error
	}{
		{
			matchers: []*labels.Matcher{
				{
					Type:  labels.MatchEqual,
					Name:  labels.MetricName,
					Value: "scrape_duration_seconds",
				},
			},
			result: `{__name__="scrape_duration_seconds"}`,
		},
		{
			matchers: []*labels.Matcher{
				{
					Type:  labels.MatchEqual,
					Name:  labels.MetricName,
					Value: "scrape_duration_seconds",
				},
				{
					Type:  labels.MatchEqual,
					Name:  "job",
					Value: "prometheus",
				},
			},
			result: `{__name__="scrape_duration_seconds",job="prometheus"}`,
		},
		{
			matchers: []*labels.Matcher{
				{
					Type:  labels.MatchRegexp,
					Name:  labels.MetricName,
					Value: ".+",
				},
			},
			result: `{__name__=~".+"}`,
		},
	}

	for _, test := range tests {
		ret, err := MatcherToString(test.matchers)
		if err != test.err {
			t.Fatalf("Mismatch in error expected=%v actual=%v", test.err, err)
		}
		if ret != test.result {
			t.Fatalf("Unexpected result expected=%v actual=%v", test.result, ret)
		}
	}
}
