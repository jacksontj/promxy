package proxyquerier

import (
	"fmt"
	"strings"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage/metric"
)

// convert list of labelMatchers to a promql string

func MatcherToString(matchers []*metric.LabelMatcher) (string, error) {
	args := make([]string, 0, len(matchers))
	var metricName string
	for _, matcher := range matchers {
		// TODO: other special names?
		if matcher.Name == model.MetricNameLabel {
			if metricName == "" {
				metricName = string(matcher.Value)
			} else {
				return "", fmt.Errorf("2 metric names? %v %v", metricName, matcher.Value)
			}
		} else {
			args = append(args, matcher.String())
		}
	}

	if len(args) > 0 {
		return metricName + "{" + strings.Join(args, ",") + "}", nil
	} else {
		return metricName, nil
	}
}
