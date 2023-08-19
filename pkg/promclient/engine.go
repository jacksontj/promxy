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
)

// StorageWarningsToAPIWarnings simply converts `storage.Warnings` to `v1.Warnings`
// which is simply converting a []error -> []string
// TODO: move to a util package?
func StorageWarningsToAPIWarnings(warnings storage.Warnings) v1.Warnings {
	ret := make(v1.Warnings, len(warnings))
	for i, w := range warnings {
		ret[i] = w.Error()
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
			for _, label := range item.Metric {
				metric[model.LabelName(label.Name)] = model.LabelValue(label.Value)
			}

			samples := make([]model.SamplePair, len(item.Points))
			for x, sample := range item.Points {
				samples[x] = model.SamplePair{
					Timestamp: model.Time(sample.T),
					Value:     model.SampleValue(sample.V),
				}
			}

			matrix[i] = &model.SampleStream{
				Metric: metric,
				Values: samples,
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
func (a *EngineAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// QueryRange performs a query for the given range.
func (a *EngineAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	engineQuery, err := a.e.NewRangeQuery(a.q, &promql.QueryOpts{false}, query, r.Start, r.End, r.Step)
	if err != nil {
		return nil, nil, err
	}

	result := engineQuery.Exec(ctx)
	if result.Err != nil {
		return nil, StorageWarningsToAPIWarnings(result.Warnings), result.Err
	}

	val, err := ParserValueToModelValue(result.Value)
	if err != nil {
		return nil, nil, err
	}

	return val, StorageWarningsToAPIWarnings(result.Warnings), nil
}

// Series finds series by label matchers.
func (a *EngineAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *EngineAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (a *EngineAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, fmt.Errorf("not implemented")
}
