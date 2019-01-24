package promcache

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
)

// CacheClientOptions contains all the options for creating a CacheClient
// this includes options specific to the client as well as options to create
// the actual caching layer
type CacheClientOptions struct {
	// Client options
	StepsPerBucket int `yaml:"steps_per_bucket"`

	// Cache options
	CachePlugin  string                 `yaml:"cache_plugin"`
	CacheOptions map[string]interface{} `yaml:"cache_options"`
}

// NewCacheClient creates a CacheClient with appropriate cache based on the given options
func NewCacheClient(key []byte, o CacheClientOptions, a promclient.API) (*CacheClient, error) {
	cClient := &CacheClient{API: a, o: o, k: key}

	cache, err := New(o.CachePlugin, o.CacheOptions, cClient)
	if err != nil {
		return nil, errors.Wrap(err, "error creating cache plugin")
	}
	cClient.c = cache

	return cClient, nil
}

// CacheClient is a caching API client to prometheus.
// This implements the promclient.API interface, and as such can be used interchangeably
type CacheClient struct {
	promclient.API
	o CacheClientOptions
	c Cache
	k []byte
}

func (c *CacheClient) Get(ctx context.Context, k CacheKey) (model.Value, error) {
	switch k.Func {
	case Func_QUERY_RANGE:
		start := time.Unix(0, k.Start)
		r := v1.Range{
			Start: start,
			End:   start.Add(time.Nanosecond * time.Duration(k.BucketSize)),
			Step:  time.Nanosecond * time.Duration(k.StepSize),
		}
		return c.API.QueryRange(ctx, k.Query, r)
	default:
		return nil, fmt.Errorf("unsupported func %s", k.Func)
	}
}

// Query performs a query for the given time.
func (c *CacheClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	bucketSize := r.Step * time.Duration(c.o.StepsPerBucket)
	var matrix model.Value

	// Offset within the normalized step
	stepOffset := r.Start.Sub(r.Start.Truncate(r.Step))
	// Start by truncating to the bucket size, this is the
	start := r.Start.Truncate(bucketSize).Add(stepOffset)

	// TODO: parallelize / configurable
	for start.Before(r.End) {
		nextBucket := start.Add(bucketSize)

		// Cache key for range
		key := CacheKey{
			ServerGroupKey: c.k,
			Func:           Func_QUERY_RANGE,
			Query:          query,
			Start:          start.UnixNano(),
			BucketSize:     bucketSize.Nanoseconds(),
			StepOffset:     stepOffset.Nanoseconds(),
			StepSize:       r.Step.Nanoseconds(),
		}
		v, err := c.c.Get(ctx, key)
		if err != nil {
			return nil, err
		}
		matrix, err = promhttputil.MergeValues(model.Time(0), matrix, v)
		if err != nil {
			return nil, err
		}
		for start.Before(nextBucket) {
			start = start.Add(r.Step)
		}
	}

	rangeStart := model.TimeFromUnixNano(r.Start.UnixNano())
	rangeEnd := model.TimeFromUnixNano(r.End.UnixNano())

	TrimMatrix(matrix.(model.Matrix), rangeStart, rangeEnd)
	return matrix, nil
}
