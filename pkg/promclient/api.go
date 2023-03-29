package promclient

import (
	"context"
	"fmt"
	"math"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage/remote"

	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// Copied from prometheus' API (these should just be exported...)
// TODO: switch to model values once they are fixed
//
//	https://github.com/prometheus/client_golang/issues/951
var (
	minTime = time.Unix(math.MinInt64/1000+62135596801, 0).UTC()
	maxTime = time.Unix(math.MaxInt64/1000-62135596801, 999999999).UTC().Truncate(time.Millisecond) // Truncated to ms because prometheus' web API does this
)

// PromAPIV1 implements our internal API interface using *only* the v1 HTTP API
// Simply wraps the prom API to fullfil our internal API interface
type PromAPIV1 struct {
	v1.API
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (p *PromAPIV1) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	// If the start or end are min/max (respectively) we want to un-set before we send those down
	if startTime.Equal(minTime) {
		startTime = time.Time{}
	}
	if endTime.Equal(maxTime) {
		endTime = time.Time{}
	}

	return p.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (p *PromAPIV1) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	// If the start or end are min/max (respectively) we want to un-set before we send those down
	if startTime.Equal(minTime) {
		startTime = time.Time{}
	}
	if endTime.Equal(maxTime) {
		endTime = time.Time{}
	}

	return p.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (p *PromAPIV1) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	return p.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (p *PromAPIV1) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	return p.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (p *PromAPIV1) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	// If the start or end are min/max (respectively) we want to un-set before we send those down
	if startTime.Equal(minTime) {
		startTime = time.Time{}
	}
	if endTime.Equal(maxTime) {
		endTime = time.Time{}
	}

	return p.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (p *PromAPIV1) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := promhttputil.MatcherToString(matchers)
	if err != nil {
		return nil, nil, err
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
	remote.ReadClient
}

// GetValue loads the raw data for a given set of matchers in the time range
func (p *PromAPIRemoteRead) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	query, err := remote.ToQuery(int64(timestamp.FromTime(start)), int64(timestamp.FromTime(end)), matchers, nil)
	if err != nil {
		return nil, nil, err
	}
	result, err := p.ReadClient.Read(ctx, query)
	if err != nil {
		return nil, nil, err
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

	return matrix, nil, nil
}
