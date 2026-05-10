package promclient

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// AnnotationsToAPIWarnings converts annotations.Annotations to v1.Warnings.
// TODO: move to a util package?
func AnnotationsToAPIWarnings(anns annotations.Annotations) v1.Warnings {
	if len(anns) == 0 {
		return nil
	}
	ret := make(v1.Warnings, 0, len(anns))
	for _, w := range anns {
		ret = append(ret, w.Error())
	}
	return ret
}

// ParserValueToModelValue can *parser.Value to model.Value
func ParserValueToModelValue(value parser.Value) (model.Value, error) {
	switch v := value.(type) {
	case promql.Matrix:
		matrix := make(model.Matrix, v.Len())
		for i, item := range v {
			metric := make(model.Metric)
			item.Metric.Range(func(label labels.Label) {
				metric[model.LabelName(label.Name)] = model.LabelValue(label.Value)
			})

			samples := make([]model.SamplePair, len(item.Floats))
			for x, sample := range item.Floats {
				samples[x] = model.SamplePair{
					Timestamp: model.Time(sample.T),
					Value:     model.SampleValue(sample.F),
				}
			}

			var histograms []model.SampleHistogramPair
			if len(item.Histograms) > 0 {
				histograms = make([]model.SampleHistogramPair, len(item.Histograms))
				for x, p := range item.Histograms {
					histograms[x] = model.SampleHistogramPair{
						Timestamp: model.Time(p.T),
						Histogram: floatHistogramToSampleHistogram(p.H),
					}
				}
			}

			matrix[i] = &model.SampleStream{
				Metric:     metric,
				Values:     samples,
				Histograms: histograms,
			}
		}
		return matrix, nil
	default:
		return nil, fmt.Errorf("unknown type %T", v)
	}
}

// NewEngineAPI returns a new EngineAPI
func NewEngineAPI(e *promql.Engine, q storage.Queryable) (*EngineAPI, error) {
	return &EngineAPI{
		e: e,
		q: q,
	}, nil
}

// EngineAPI implements the API interface using a Queryable and an engine
type EngineAPI struct {
	e *promql.Engine
	q storage.Queryable
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (a *EngineAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// LabelValues performs a query for the values of the given label.
func (a *EngineAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// Query performs a query for the given time.
func (a *EngineAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return storage.ErrSeriesSet(fmt.Errorf("not implemented"))
}

// QueryRange performs a query for the given range.
func (a *EngineAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	engineQuery, err := a.e.NewRangeQuery(ctx, a.q, promql.NewPrometheusQueryOpts(false, 0), query, r.Start, r.End, r.Step)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	result := engineQuery.Exec(ctx)
	if result.Err != nil {
		return promapi.NewSeriesSet(nil, result.Warnings, result.Err)
	}

	val, err := ParserValueToModelValue(result.Value)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	return ModelValueToSeriesSet(val, result.Warnings, nil)
}

// Series finds series by label matchers.
func (a *EngineAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *EngineAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return storage.ErrSeriesSet(fmt.Errorf("not implemented"))
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (a *EngineAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, fmt.Errorf("not implemented")
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (a *EngineAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, fmt.Errorf("not implemented")
}
