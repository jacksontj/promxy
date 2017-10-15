package proxyquerier

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/metric"
)

func BenchmarkMatcherToString(b *testing.B) {
	matchers := []*metric.LabelMatcher{
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
	}
	for n := 0; n < b.N; n++ {
		MatcherToString(matchers)
	}

}
