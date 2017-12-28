package servergroup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
)

type ServerGroups []*ServerGroup

func (s ServerGroups) GetData(ctx context.Context, path string, values url.Values, client *http.Client) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan model.Value, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func() {
			result, err := serverGroup.GetData(childContext, path, values, client)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}()
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
				result, err = promhttputil.MergeValues(result, childResult)
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

func (s ServerGroups) GetSeries(ctx context.Context, path string, values url.Values, client *http.Client) (*promclient.SeriesResult, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan *promclient.SeriesResult, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func() {
			result, err := serverGroup.GetSeries(childContext, path, values, client)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}()
	}

	// Wait for results as we get them
	result := &promclient.SeriesResult{}
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

func (s ServerGroups) GetValuesForLabelName(ctx context.Context, path string, client *http.Client) (*promclient.LabelResult, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan *promclient.LabelResult, len(s))
	errChan := make(chan error, len(s))

	// Scatter out all the queries
	for _, serverGroup := range s {
		go func() {
			result, err := serverGroup.GetValuesForLabelName(childContext, path, client)
			if err != nil {
				errChan <- err
			} else {
				resultChan <- result
			}
		}()
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
