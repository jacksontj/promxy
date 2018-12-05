package servergroup

import (
	"context"
	"net/url"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type ServerGroups []*ServerGroup

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

func (s ServerGroups) GetData(ctx context.Context, path string, values url.Values) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	resultChans := make([]chan interface{}, len(s))

	// Scatter out all the queries
	for i, serverGroup := range s {
		resultChans[i] = make(chan interface{}, 1)
		go func(retChan chan interface{}, serverGroup *ServerGroup) {
			result, err := serverGroup.GetData(childContext, path, values)
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

func (s ServerGroups) GetValuesForLabelName(ctx context.Context, path string) ([]model.LabelValue, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(s))

	// Scatter out all the queries
	for i, serverGroup := range s {
		resultChans[i] = make(chan interface{}, 1)
		go func(retChan chan interface{}, serverGroup *ServerGroup) {
			result, err := serverGroup.GetValuesForLabelName(childContext, path)
			if err != nil {
				retChan <- err
			} else {
				retChan <- result
			}
		}(resultChans[i], serverGroup)
	}

	// Wait for results as we get them
	var result []model.LabelValue
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
			case []model.LabelValue:
				if result == nil {
					result = retTyped
				} else {
					result = promclient.MergeLabelValues(result, retTyped)
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

func (s ServerGroups) GetSeries(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(s))

	// Scatter out all the queries
	for i, serverGroup := range s {
		resultChans[i] = make(chan interface{}, 1)
		go func(retChan chan interface{}, serverGroup *ServerGroup) {
			result, err := serverGroup.GetSeries(childContext, start, end, matchers)
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
