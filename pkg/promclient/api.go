package promclient

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage"
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
// Simply wraps the prom API to fullfil our internal API interface.
//
// Client is the underlying api.Client used for Query / QueryRange. It is set
// when a PromAPIV1 is constructed against a real downstream so that we can
// parse the `infos` field of the JSON response (which client_golang's v1
// package drops). Query / QueryRange fall back to the embedded v1.API if
// Client is nil — that path is used by tests that supply a stub v1.API
// directly.
type PromAPIV1 struct {
	v1.API
	Client api.Client
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
func (p *PromAPIV1) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if hasNegativeFractionalSecond(ts) {
		return storage.ErrSeriesSet(errNegativeFractionalTimestamp)
	}
	if p.Client == nil {
		v, w, err := p.API.Query(ctx, query, ts)
		return ModelValueToSeriesSet(v, promhttputil.WarningsConvert(w), err)
	}
	u := p.Client.URL(epQuery, nil)
	q := u.Query()
	q.Set("query", query)
	if !ts.IsZero() {
		q.Set("time", formatAPITime(ts))
	}
	return queryWithInfos(ctx, p.Client, u, q)
}

// QueryRange performs a query for the given range.
func (p *PromAPIV1) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	// Reject ranges whose first eval step would land on a pre-epoch
	// sub-second timestamp; the upstream JSON decoder mis-parses these.
	// See hasNegativeFractionalSecond. Step times are start + k*step, so
	// checking start covers the whole range when step is whole-second
	// (the only thing PromQL produces here).
	if hasNegativeFractionalSecond(r.Start) {
		return storage.ErrSeriesSet(errNegativeFractionalTimestamp)
	}
	if p.Client == nil {
		v, w, err := p.API.QueryRange(ctx, query, r)
		return ModelValueToSeriesSet(v, promhttputil.WarningsConvert(w), err)
	}
	u := p.Client.URL(epQueryRange, nil)
	q := u.Query()
	q.Set("query", query)
	q.Set("start", formatAPITime(r.Start))
	q.Set("end", formatAPITime(r.End))
	q.Set("step", strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64))
	return queryWithInfos(ctx, p.Client, u, q)
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
func (p *PromAPIV1) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	pql, err := promhttputil.MatcherToString(matchers)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}

	// We want to grab only the raw datapoints, so we do that through the query
	// interface passing in a duration that is at least as long as ours (the
	// added second deals with float rounding when casting to int64).
	query := pql + fmt.Sprintf("[%ds]", int64(end.Sub(start).Seconds())+1)
	return p.Query(ctx, query, end)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (p *PromAPIV1) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return p.API.QueryExemplars(ctx, query, startTime, endTime)
}

// PromAPIRemoteRead implements our internal API interface using a combination of
// the v1 HTTP API and the "experimental" remote_read API
type PromAPIRemoteRead struct {
	API
	remote.ReadClient
}

// GetValue loads the raw data for a given set of matchers in the time range
func (p *PromAPIRemoteRead) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	query, err := remote.ToQuery(int64(timestamp.FromTime(start)), int64(timestamp.FromTime(end)), matchers, nil)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	// remote.Read returns a storage.SeriesSet, but the streamed/chunked variant
	// is lazy: it reads from the HTTP body (and holds the request context) until
	// iterated. We must drain it here, while the context is still alive --
	// otherwise iteration downstream races the context cancel and fails with
	// "context canceled". materializeSeriesSet copies every sample so the result
	// is safe to consume after this returns.
	ss, err := p.ReadClient.Read(ctx, query, false)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return materializeSeriesSet(ss)
}
