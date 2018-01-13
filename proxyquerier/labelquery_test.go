package proxyquerier

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/metric"
)

func TestMatcherToString(t *testing.T) {
	tests := []struct {
		matchers []*metric.LabelMatcher
		result   string
		err      error
	}{
		{
			matchers: []*metric.LabelMatcher{
				{
					Type:  metric.Equal,
					Name:  model.MetricNameLabel,
					Value: "scrape_duration_seconds",
				},
			},
			result: `{__name__="scrape_duration_seconds"}`,
		},
		{
			matchers: []*metric.LabelMatcher{
				{
					Type:  metric.Equal,
					Name:  model.MetricNameLabel,
					Value: "scrape_duration_seconds",
				},
				{
					Type:  metric.Equal,
					Name:  "job",
					Value: "prometheus",
				},
			},
			result: `{__name__="scrape_duration_seconds",job="prometheus"}`,
		},
		{
			matchers: []*metric.LabelMatcher{
				{
					Type:  metric.RegexMatch,
					Name:  model.MetricNameLabel,
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
