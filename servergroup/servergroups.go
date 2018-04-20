package servergroup

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type ServerGroups []*ServerGroup

// GetValue fetches a `model.Value` from the servergroups
func (s ServerGroups) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan model.Value, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func(serverGroup *ServerGroup) {
			result, err := serverGroup.GetValue(childContext, start, end, matchers)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}(serverGroup)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(s); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			lastError = err
			errCount++
		case childResult := <-resultChan:
			var err error
			if result == nil {
				result = childResult
			} else {
				result, err = promhttputil.MergeValues(model.TimeFromUnix(0), result, childResult)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(s) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

func (s ServerGroups) GetData(ctx context.Context, path string, values url.Values) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan model.Value, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func(serverGroup *ServerGroup) {
			result, err := serverGroup.GetData(childContext, path, values)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}(serverGroup)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(s); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			lastError = err
			errCount++
		case childResult := <-resultChan:
			var err error
			if result == nil {
				result = childResult
			} else {
				result, err = promhttputil.MergeValues(model.TimeFromUnix(0), result, childResult)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(s) {
		fmt.Println(errCount, len(s))
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

func (s ServerGroups) GetValuesForLabelName(ctx context.Context, path string) (*promclient.LabelResult, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan *promclient.LabelResult, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func(serverGroup *ServerGroup) {
			result, err := serverGroup.GetValuesForLabelName(childContext, path)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}(serverGroup)
	}

	// Wait for results as we get them
	result := &promclient.LabelResult{}
	var lastError error
	errCount := 0
	for i := 0; i < len(s); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			lastError = err
			errCount++
		case childResult := <-resultChan:
			if result == nil {
				result = childResult
			} else {
				if err := result.Merge(childResult); err != nil {
					return nil, err
				}
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(s) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}
