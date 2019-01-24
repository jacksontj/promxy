package promclient

import (
	"context"
	"fmt"
	"time"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

type FetcherFunc func(ctx context.Context) (model.Value, error)

func (f FetcherFunc) Fetch(ctx context.Context) (model.Value, error) {
	return f(ctx)
}

type Fetcher interface {
	Fetch(ctx context.Context) (model.Value, error)
}

type MetricFetcher interface {
	Fetcher
	RecordMetric(api, status string, took float64)
}

func MultiFetch(ctx context.Context, antiAffinity model.Time, fetchers ...Fetcher) (model.Value, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(fetchers))

	for i, f := range fetchers {
		resultChans[i] = make(chan interface{}, 1)
		go func(i int, retChan chan interface{}, f Fetcher) {
			start := time.Now()
			result, err := f.Fetch(childContext)
			took := time.Now().Sub(start)
			if err != nil {
				if mf, ok := f.(MetricFetcher); ok {
					mf.RecordMetric("query", "error", took.Seconds())
				}
				retChan <- NormalizePromError(err)
			} else {
				if mf, ok := f.(MetricFetcher); ok {
					mf.RecordMetric("query", "success", took.Seconds())
				}
				retChan <- result
			}
		}(i, resultChans[i], f)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(fetchers); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(antiAffinity, result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			case nil:
				continue
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(fetchers) {
		return nil, errors.Wrap(lastError, "Unable to fetch from downstream servers")
	}

	return result, nil
}
