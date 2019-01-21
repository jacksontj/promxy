package promcache

import (
	"context"
	"strconv"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/karlseguin/ccache"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

var (
	// TODO: move into cacheClient
	// TODO: make an interface
	cache = ccache.New(ccache.Configure().MaxSize(1000).ItemsToPrune(100))

	// TODO: move into cacheClient
	// TODO: config
	// how many steps per bucket
	stepsPerBucket = 2
)

// CacheClient is a caching API client to prometheus.
// This implements the promclient.API interface, and as such can be used interchangeably
type CacheClient struct {
	promclient.API
}

func (c *CacheClient) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	bucketSize := r.Step * time.Duration(stepsPerBucket)
	var matrix model.Value
	start := r.Start.Truncate(bucketSize)

	// TODO: parallelize / configurable
	for start.Before(r.End) {
		nextBucket := start.Add(bucketSize)

		v, err := c.innerQueryRange(ctx, bucketSize, query, v1.Range{Start: start, End: start.Add(bucketSize), Step: r.Step})
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

// innerQueryRange gets queries that are within a bucket, we specifically want to query all data within that bucket
func (c *CacheClient) innerQueryRange(ctx context.Context, bucketSize time.Duration, query string, r v1.Range) (model.Value, error) {
	// Cache key for range
	// Cache key is query_range:starttime_bucketsize:query:step
	key := "query_range:" + strconv.FormatInt(r.Start.Unix(), 10) + "_" + strconv.FormatFloat(bucketSize.Seconds(), 'f', -1, 64) + ":" + query + ":" + strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64)

	item, err := cache.Fetch(key, time.Minute*10, func() (interface{}, error) {
		return c.API.QueryRange(ctx, query, r)
	})

	if err != nil {
		return nil, err
	}

	return item.Value().(model.Value), nil
}
