package servergroup

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"

	"github.com/jacksontj/promxy/dedupe"
	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type ServerGroups []*ServerGroup

var c = dedupe.NewDedupeController(10)

// GetValue fetches a `model.Value` from the servergroups
func (s ServerGroups) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	b, _ := json.Marshal([]interface{}{"GetValue", start, end, matchers})
	key := string(b)

	result := c.JobForKey(key, "GetValue", func() interface{} {
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
				return ctx.Err()
			case ret := <-resultChans[i]:
				switch retTyped := ret.(type) {
				case error:
					lastError = retTyped
					errCount++
				case model.Value:
					var err error
					result, err = promhttputil.MergeValues(model.TimeFromUnix(0), result, retTyped)
					if err != nil {
						return err
					}
				}
			}

		}

		// If we got only errors, lets return that
		if errCount == len(s) {
			return fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
		}

		return result
	})

	switch resultTyped := result.(type) {
	case model.Value:
		return resultTyped, nil
	case error:
		return nil, resultTyped
	default:
		return nil, fmt.Errorf("Unknown return type")
	}
}

func (s ServerGroups) GetData(ctx context.Context, path string, values url.Values) (model.Value, error) {
	key := "GetData" + path + values.Encode()

	result := c.JobForKey(key, "GetData", func() interface{} {
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
				return ctx.Err()
			case ret := <-resultChans[i]:
				switch retTyped := ret.(type) {
				case error:
					lastError = retTyped
					errCount++
				case model.Value:
					var err error
					result, err = promhttputil.MergeValues(model.TimeFromUnix(0), result, retTyped)
					if err != nil {
						return err
					}
				}
			}
		}

		// If we got only errors, lets return that
		if errCount == len(s) {
			return fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
		}

		return result
	})

	switch resultTyped := result.(type) {
	case model.Value:
		return resultTyped, nil
	case error:
		return nil, resultTyped
	default:
		return nil, fmt.Errorf("Unknown return type")
	}
}

func (s ServerGroups) GetValuesForLabelName(ctx context.Context, path string) (*promclient.LabelResult, error) {
	key := "GetValuesForLabelName" + path

	result := c.JobForKey(key, "GetValuesForLabelName", func() interface{} {
		childContext, childContextCancel := context.WithCancel(ctx)
		defer childContextCancel()

		resultChans := make([]chan interface{}, len(s))

		resultChan := make(chan *promclient.LabelResult, len(s))
		errChan := make(chan error, len(s))

		// Scatter out all the queries
		for i, serverGroup := range s {
			resultChans[i] = make(chan interface{}, 1)
			go func(retChan chan interface{}, serverGroup *ServerGroup) {
				result, err := serverGroup.GetValuesForLabelName(childContext, path)
				if err != nil {
					errChan <- err
				} else {
					resultChan <- result
				}
			}(resultChans[i], serverGroup)
		}

		// Wait for results as we get them
		result := &promclient.LabelResult{}
		var lastError error
		errCount := 0
		for i := 0; i < len(s); i++ {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case ret := <-resultChans[i]:
				switch retTyped := ret.(type) {
				case error:
					lastError = retTyped
					errCount++
				case *promclient.LabelResult:
					if result == nil {
						result = retTyped
					} else {
						if err := result.Merge(retTyped); err != nil {
							return err
						}
					}
				}
			}
		}

		// If we got only errors, lets return that
		if errCount == len(s) {
			return fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
		}

		return result

	})

	switch resultTyped := result.(type) {
	case *promclient.LabelResult:
		return resultTyped, nil
	case error:
		return nil, resultTyped
	default:
		return nil, fmt.Errorf("Unknown return type")
	}
}
