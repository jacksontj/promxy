package promclient

import (
	"context"
	"runtime"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/sirupsen/logrus"
)

// ConvertErr converts the errors that the prometheus API client returns
// into errors that the prometheus API server actually handles and returns proper
// error codes for
func ConvertErr(err error, memExceeded bool) error {
	// If error is caused by memory exceeds
	if memExceeded {
		return errors.New("Memory Consumption Limit Exceeded")
	}
	// If all else fails, return the original error
	return err
}

// MemMonitorAPI sets up a memory monitor for a given api operation
type MemMonitorAPI struct {
	API
	MaxMem int
}

// Starts a memory monitoring goroutine that oversees the memory usage of the process during a query
// Cancels all queries when the memory threshold is exceeded
// Takes in the parent's context cancel function used for stopping the query
func (m *MemMonitorAPI) Monitor(ctx context.Context, parentContextCancel func(), memExceeded *bool) {
	var memAlloc uint64
	d := 5 * time.Second
	MaxMem := uint64(m.MaxMem)
	for x := range time.Tick(d) {
		select {
		case <-ctx.Done():
			logrus.Debugf("Memory Consumed = %v MiB  %v\n", memAlloc, x)
			return
		default:
			var mem runtime.MemStats
			runtime.ReadMemStats(&mem)
			memAlloc := mem.Sys / 1024 / 1024

			if memAlloc > MaxMem {
				*memExceeded = true
				parentContextCancel()
				logrus.Debugf("Memory Consumed = %v MiB  %v\n", memAlloc, x)
				return
			}
		}
	}
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (m *MemMonitorAPI) LabelNames(ctx context.Context) ([]string, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.LabelNames(childContext)
	return v, w, ConvertErr(err, memExceeded)
}

// LabelValues performs a query for the values of the given label.
func (m *MemMonitorAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.LabelValues(childContext, label)
	return v, w, ConvertErr(err, memExceeded)
}

// Query performs a query for the given time.
func (m *MemMonitorAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.Query(childContext, query, ts)
	return v, w, ConvertErr(err, memExceeded)
}

// QueryRange performs a query for the given range.
func (m *MemMonitorAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.QueryRange(childContext, query, r)
	return v, w, ConvertErr(err, memExceeded)
}

// Series finds series by label matchers.
func (m *MemMonitorAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.Series(childContext, matches, startTime, endTime)
	return v, w, ConvertErr(err, memExceeded)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (m *MemMonitorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()

	memExceeded := false
	go m.Monitor(childContext, childContextCancel, &memExceeded)

	v, w, err := m.API.GetValue(childContext, start, end, matchers)
	return v, w, ConvertErr(err, memExceeded)
}
