package servergroup

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/jacksontj/promxy/promclient"
)

type ServerGroups []*ServerGroup

func (s ServerGroups) getAPIs() []promclient.API {
	apis := make([]promclient.API, len(s))
	for i, serverGroup := range s {
		apis[i] = serverGroup
	}
	return apis
}

// Query performs a query for the given time.
func (s ServerGroups) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s ServerGroups) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).QueryRange(ctx, query, r)
}

// GetValue fetches a `model.Value` from the servergroups
func (s ServerGroups) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).GetValue(ctx, start, end, matchers)
}

func (s ServerGroups) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).LabelValues(ctx, label)
}

// Series finds series by label matchers.
func (s ServerGroups) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).Series(ctx, matches, startTime, endTime)
}
