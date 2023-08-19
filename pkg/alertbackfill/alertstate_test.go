package alertbackfill

import (
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

func TestGenerateAlertStateMatrix(t *testing.T) {
	start := model.Time(0).Add(time.Minute)
	tests := []struct {
		in          model.Matrix
		matchers    []*labels.Matcher
		alertLabels labels.Labels
		step        time.Duration

		out model.Matrix
	}{
		// Simple test case
		{
			in: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"__name__": "prometheus_build_info",
						"job":      "prometheus",
						"replica":  "a",
					},
					Values: []model.SamplePair{
						{start.Add(time.Minute * 0), 1},
						{start.Add(time.Minute * 1), 1},
						{start.Add(time.Minute * 2), 1},
						{start.Add(time.Minute * 3), 1},
						{start.Add(time.Minute * 4), 1},
						// Have a gap!
						{start.Add(time.Minute * 6), 1},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"__name__": "prometheus_build_info",
						"job":      "prometheus",
						"replica":  "b",
					},
					Values: []model.SamplePair{
						{start.Add(time.Minute * 0), 1},
						{start.Add(time.Minute * 1), 1},
						{start.Add(time.Minute * 2), 1},
						{start.Add(time.Minute * 3), 1},
						{start.Add(time.Minute * 4), 1},
					},
				},
			},
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "testalert"),
				labels.MustNewMatcher(labels.MatchEqual, "replica", "a"),
			},
			alertLabels: labels.Labels{
				labels.Label{"severity", "page"},
			},
			step: time.Minute,
			out: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"__name__":  "ALERTS_FOR_STATE",
						"alertname": "testalert",
						"job":       "prometheus",
						"replica":   "a",
						"severity":  "page",
					},
					Values: []model.SamplePair{
						{start.Add(time.Minute * 0), model.SampleValue(start.Unix())},
						{start.Add(time.Minute * 1), model.SampleValue(start.Unix())},
						{start.Add(time.Minute * 2), model.SampleValue(start.Unix())},
						{start.Add(time.Minute * 3), model.SampleValue(start.Unix())},
						{start.Add(time.Minute * 4), model.SampleValue(start.Unix())},
						{start.Add(time.Minute * 6), model.SampleValue(start.Add(time.Minute * 6).Unix())},
					},
				},
			},
		},

		// Example from `prometheus_build_info == 1`
		{
			in: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"__name__":  "prometheus_build_info",
						"branch":    "HEAD",
						"goversion": "go1.16.4",
						"instance":  "demo.do.prometheus.io:9090",
						"job":       "prometheus",
						"replica":   "a",
						"revision":  "24c9b61221f7006e87cd62b9fe2901d43e19ed53",
						"version":   "2.27.0",
					},
					Values: []model.SamplePair{
						{model.TimeFromUnix(1692419110), 1},
						{model.TimeFromUnix(1692419115), 1},
						{model.TimeFromUnix(1692419120), 1},
						{model.TimeFromUnix(1692419125), 1},
						{model.TimeFromUnix(1692419130), 1},
					},
				},
				&model.SampleStream{
					Metric: model.Metric{
						"__name__":  "prometheus_build_info",
						"branch":    "HEAD",
						"goversion": "go1.16.4",
						"instance":  "demo.do.prometheus.io:9090",
						"job":       "prometheus",
						"replica":   "b",
						"revision":  "24c9b61221f7006e87cd62b9fe2901d43e19ed53",
						"version":   "2.27.0",
					},
					Values: []model.SamplePair{
						{model.TimeFromUnix(1692419110), 1},
						{model.TimeFromUnix(1692419115), 1},
						{model.TimeFromUnix(1692419120), 1},
						{model.TimeFromUnix(1692419125), 1},
						{model.TimeFromUnix(1692419130), 1},
					},
				},
			},
			matchers: []*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, model.MetricNameLabel, "ALERTS_FOR_STATE"),
				labels.MustNewMatcher(labels.MatchEqual, model.AlertNameLabel, "testAlert"),
				labels.MustNewMatcher(labels.MatchEqual, "branch", "HEAD"),
				labels.MustNewMatcher(labels.MatchEqual, "goversion", "go1.16.4"),
				labels.MustNewMatcher(labels.MatchEqual, "instance", "demo.do.prometheus.io:9090"),
				labels.MustNewMatcher(labels.MatchEqual, "job", "prometheus"),
				labels.MustNewMatcher(labels.MatchEqual, "replica", "a"),
				labels.MustNewMatcher(labels.MatchEqual, "revision", "24c9b61221f7006e87cd62b9fe2901d43e19ed53"),
				labels.MustNewMatcher(labels.MatchEqual, "severity", "pageMORE"),
				labels.MustNewMatcher(labels.MatchEqual, "version", "2.27.0"),
			},
			alertLabels: labels.Labels{
				labels.Label{"severity", "pageMORE"},
			},
			step: time.Second * 5,
			out: model.Matrix{
				&model.SampleStream{
					Metric: model.Metric{
						"__name__":  "ALERTS_FOR_STATE",
						"alertname": "testAlert",
						"branch":    "HEAD",
						"goversion": "go1.16.4",
						"instance":  "demo.do.prometheus.io:9090",
						"job":       "prometheus",
						"replica":   "a",
						"revision":  "24c9b61221f7006e87cd62b9fe2901d43e19ed53",
						"version":   "2.27.0",
						"severity":  "pageMORE",
					},
					Values: []model.SamplePair{
						{model.TimeFromUnix(1692419110), 1692419110},
						{model.TimeFromUnix(1692419115), 1692419110},
						{model.TimeFromUnix(1692419120), 1692419110},
						{model.TimeFromUnix(1692419125), 1692419110},
						{model.TimeFromUnix(1692419130), 1692419110},
					},
				},
			},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			out := GenerateAlertStateMatrix(test.in, test.matchers, test.alertLabels, test.step)

			if test.out.String() != out.String() {
				t.Fatalf("mismatch in series expected=%v actual=%v", test.out, out)
			}

			for i, sampleStream := range out {
				for _, matcher := range test.matchers {
					if !matcher.Matches(string(sampleStream.Metric[model.LabelName(matcher.Name)])) {
						t.Fatalf("out series=%d label %s=%s doesn't match matcher %v", i, matcher.Name, sampleStream.Metric[model.LabelName(matcher.Name)], matcher.Value)
					}
				}
			}
		})
	}
}
