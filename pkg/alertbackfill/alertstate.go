package alertbackfill

import (
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// GenerateAlertStateMatrix will generate the a model.Matrix which is the equivalent
// of the `ALERTS_FOR_STATE` series that would have been generated for the given data
func GenerateAlertStateMatrix(v model.Matrix, matchers []*labels.Matcher, alertLabels labels.Labels, step time.Duration) model.Matrix {
	matrix := make(model.Matrix, 0, v.Len())
MATRIXSAMPLE_LOOP:
	for _, item := range v {
		// clone the metric -- and convert the labels
		metric := item.Metric.Clone()

		// Add the labels which the alert would add
		for _, label := range alertLabels {
			metric[model.LabelName(label.Name)] = model.LabelValue(label.Value)
		}

		// Filter to results that match our matchers
		for _, matcher := range matchers {
			switch matcher.Name {
			// Overwrite the __name__ and alertname
			case model.MetricNameLabel, model.AlertNameLabel:
				metric[model.LabelName(matcher.Name)] = model.LabelValue(matcher.Value)
			default:
				if !matcher.Matches(string(metric[model.LabelName(matcher.Name)])) {
					continue MATRIXSAMPLE_LOOP
				}
			}
		}

		var (
			activeAt  model.SampleValue
			lastPoint time.Time
		)
		// Now we have to convert the *actual* result into the series that is stored for the ALERTS
		samples := make([]model.SamplePair, len(item.Values))
		for x, sample := range item.Values {
			sampleTime := sample.Timestamp.Time()

			// If we are missing a point in the matrix; then we are going to assume
			// that the series cleared, so we need to reset activeAt
			if sampleTime.Sub(lastPoint) > step {
				activeAt = 0
			}
			lastPoint = sampleTime

			// if there is no `activeAt` set; lets set this timestamp (earliest timestamp in the steps that has a point)
			if activeAt == 0 {
				activeAt = model.SampleValue(sample.Timestamp.Unix())
			}

			samples[x] = model.SamplePair{
				Timestamp: sample.Timestamp,
				// The timestamp is a unix timestapm of ActiveAt, so we'll set this to the timestamp instead of the value
				Value: activeAt,
			}
		}

		matrix = append(matrix, &model.SampleStream{
			Metric: metric,
			Values: samples,
		})
	}

	return matrix
}
