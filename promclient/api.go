package promclient

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/storage/remote"

	"github.com/jacksontj/promxy/promhttputil"
)

// PromAPI implements our internal API interface using *only* the v1 HTTP API
// Simply wraps the prom API to fullfil our internal API interface
type PromAPIV1 struct {
	v1.API
}

// LabelValues performs a query for the values of the given label.
func (p *PromAPIV1) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return p.API.LabelValues(ctx, label)
}

// Query performs a query for the given time.
func (p *PromAPIV1) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	return p.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (p *PromAPIV1) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	return p.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (p *PromAPIV1) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	return p.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (p *PromAPIV1) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := promhttputil.MatcherToString(matchers)
	if err != nil {
		return nil, err
	}

	// We want to grab only the raw datapoints, so we do that through the query interface
	// passing in a duration that is at least as long as ours (the added second is to deal
	// with any rounding error etc since the duration is a floating point and we are casting
	// to an int64
	query := pql + fmt.Sprintf("[%ds]", int64(end.Sub(start).Seconds())+1)
	return p.API.Query(ctx, query, end)
}

// PromAPIRemoteRead implements our internal API interface using a combination of
// the v1 HTTP API and the "experimental" remote_read API
type PromAPIRemoteRead struct {
	API
	*remote.Client
}

func (p *PromAPIRemoteRead) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	query, err := remote.ToQuery(int64(timestamp.FromTime(start)), int64(timestamp.FromTime(end)), matchers, nil)
	if err != nil {
		return nil, err
	}
	result, err := p.Client.Read(ctx, query)
	if err != nil {
		return nil, err
	}

	// convert result (timeseries) to SampleStream
	matrix := make(model.Matrix, len(result.Timeseries))
	for i, ts := range result.Timeseries {
		metric := make(model.Metric)
		for _, label := range ts.Labels {
			metric[model.LabelName(label.Name)] = model.LabelValue(label.Value)
		}

		samples := make([]model.SamplePair, len(ts.Samples))
		for x, sample := range ts.Samples {
			samples[x] = model.SamplePair{
				Timestamp: model.Time(sample.Timestamp),
				Value:     model.SampleValue(sample.Value),
			}
		}

		matrix[i] = &model.SampleStream{
			Metric: metric,
			Values: samples,
		}
	}

	return matrix, nil
}
