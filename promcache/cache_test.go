package promcache

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	webv1 "github.com/prometheus/prometheus/web/api/v1"
)

var cacheTestData = `
load 10s
	http_requests{job="api-server", instance="0", group="production"}	0+10x1000 100+30x1000
	http_requests{job="api-server", instance="1", group="production"}	0+20x1000 200+30x1000
	http_requests{job="api-server", instance="0", group="canary"}		0+30x1000 300+80x1000
	http_requests{job="api-server", instance="1", group="canary"}		0+40x2000
`

// TODO: shared lib from test?
func startAPIForTest(storage storage.Storage, listen string) (*http.Server, chan struct{}) {
	// Start up API server for engine
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	promAPI := webv1.NewAPI(
		promql.NewEngine(nil, nil, 20, 10*time.Minute),
		storage,
		nil,
		nil,
		cfgFunc,
		nil,
		readyFunc,
		nil,
		true,
	)

	apiRouter := route.New()
	promAPI.Register(apiRouter.WithPrefix("/api/v1"))

	startChan := make(chan struct{})
	stopChan := make(chan struct{})
	srv := &http.Server{Addr: listen, Handler: apiRouter}

	go func() {
		defer close(stopChan)
		close(startChan)
		srv.ListenAndServe()
	}()

	<-startChan

	return srv, stopChan
}

// TODO: elsewhere

type countingAPI struct {
	promclient.API
	count int64
}

// LabelValues performs a query for the values of the given label.
func (c *countingAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	c.count++
	return c.API.LabelValues(ctx, label)
}

// Query performs a query for the given time.
func (c *countingAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	c.count++
	return c.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (c *countingAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	c.count++
	return c.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (c *countingAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, error) {
	c.count++
	return c.API.Series(ctx, matches, startTime, endTime)
}

var zeroTime = time.Unix(0, 0)
var cacheTests = []struct {
	query string
	v1.Range
}{
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(0 * time.Second), End: zeroTime.Add(100 * time.Second), Step: time.Second * 25},
	},
	// Repeat the same query to ensure that we don't mess with the original in cache
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(0 * time.Second), End: zeroTime.Add(100 * time.Second), Step: time.Second * 25},
	},
	// Same query, with a differnt start time
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(1 * time.Second), End: zeroTime.Add(100 * time.Second), Step: time.Second * 25},
	},
	// Do a similar query over a completely different time range
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(100 * time.Second), End: zeroTime.Add(200 * time.Second), Step: time.Second * 25},
	},
	// Do a query which has a subset of data in the bucket
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(125 * time.Second), End: zeroTime.Add(175 * time.Second), Step: time.Second * 25},
	},
	// Query which barely spans buckets but has no data
	{
		"http_requests",
		v1.Range{Start: zeroTime.Add(100 * time.Second), End: zeroTime.Add(130 * time.Second), Step: time.Second * 25},
	},
	// Query which has no result
	{
		"not_there",
		v1.Range{Start: zeroTime.Add(100 * time.Second), End: zeroTime.Add(130 * time.Second), Step: time.Second * 25},
	},
	// Query which has a scalar response
	{
		"1",
		v1.Range{Start: zeroTime.Add(0 * time.Second), End: zeroTime.Add(100 * time.Second), Step: time.Second * 25},
	},
}

func TestCache(t *testing.T) {
	promqlTest, err := promql.NewTest(t, cacheTestData)
	if err != nil {
		t.Fatalf("Error loading data: %v", err)
	}

	if err := promqlTest.Run(); err != nil {
		t.Fatalf("Error loading data: %v", err)
	}

	srv, stopChan := startAPIForTest(promqlTest.Storage(), ":9090")

	client, err := api.NewClient(api.Config{Address: "http://127.0.0.1:9090"})
	if err != nil {
		t.Fatalf("Error creating client: %v", err)
	}
	apiClient := v1.NewAPI(client)

	countClient := &countingAPI{apiClient, 0}
	opts := CacheClientOptions{
		StepsPerBucket: 3,
		CachePlugin:    "ccache",
		CacheOptions:   map[string]interface{}{},
	}
	cacheClient, err := NewCacheClient(opts, countClient)
	if err != nil {
		t.Fatalf("Error creating cacheclient: %v", err)
	}

	// Do an actual test
	ctx := context.TODO()

	for i, test := range cacheTests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			baseV, baseErr := apiClient.QueryRange(ctx, test.query, test.Range)
			v, err := cacheClient.QueryRange(ctx, test.query, test.Range)

			if baseErr != err {
				t.Fatalf("mismatch in error expected=%v actual=%v", baseErr, err)
			}

			if !reflect.DeepEqual(baseV, v) {
				t.Fatalf("Mismatch in value \nexpected=%v \nactual=%v", baseV, v)
			}

			// If it worked, hit it again, and ensure that we don't hit the API
			// (since it should cache) and that the result matches
			countBefore := countClient.count
			v2, err2 := cacheClient.QueryRange(ctx, test.query, test.Range)
			if err != err2 {
				t.Fatalf("mismatch in repeat error expected=%v actual=%v", err, err2)
			}

			if !reflect.DeepEqual(v, v2) {
				t.Fatalf("Mismatch in value \nexpected=%v \nactual=%v", v, v2)
			}

			if countClient.count > countBefore {
				t.Fatalf("Query not cached!")
			}

			// Check the datapoints, to ensure that all values are within the specified range
			matrix := v2.(model.Matrix)
			for _, stream := range matrix {
				for _, value := range stream.Values {
					if value.Timestamp.Time().Before(test.Range.Start) || value.Timestamp.Time().After(test.Range.End) {
						t.Fatalf("point %v outside of range %v %v", value, test.Range.Start, test.Range.End)
					}
				}
			}

		})
	}

	// stop server
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	<-stopChan
}
