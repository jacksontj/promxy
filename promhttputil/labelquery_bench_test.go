package promhttputil

import (
	"testing"

	"github.com/prometheus/prometheus/pkg/labels"
)

func BenchmarkMatcherToString(b *testing.B) {
	matchers := []*labels.Matcher{
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
	}
	for n := 0; n < b.N; n++ {
		MatcherToString(matchers)
	}

}
