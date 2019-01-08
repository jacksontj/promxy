package servergroup

import (
	"context"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
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
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(s))

	// Scatter out all the queries
	for i, serverGroup := range s {
		resultChans[i] = make(chan interface{}, 1)
		go func(retChan chan interface{}, serverGroup *ServerGroup) {
			result, err := serverGroup.GetValue(childContext, start, end, matchers)
			if err != nil {
				retChan <- err
			} else {
				retChan <- result
			}
		}(resultChans[i], serverGroup)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(s); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				var err error
				result, err = promhttputil.MergeValues(model.TimeFromUnix(0), result, retTyped)
				if err != nil {
					return nil, err
				}
			}
		}

	}

	// If we got only errors, lets return that
	if errCount == len(s) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}

func (s ServerGroups) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).LabelValues(ctx, label)
}

// Series finds series by label matchers.
func (s ServerGroups) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, error) {
	return promclient.NewMultiAPI(s.getAPIs(), model.TimeFromUnix(0), nil).Series(ctx, matches, startTime, endTime)
}
