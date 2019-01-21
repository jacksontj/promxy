package promcache

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/promql"
)

func BenchmarkCache(b *testing.B) {
	promqlTest, err := promql.NewTest(b, cacheTestData)
	if err != nil {
		b.Fatalf("Error loading data: %v", err)
	}

	if err := promqlTest.Run(); err != nil {
		b.Fatalf("Error loading data: %v", err)
	}

	srv, stopChan := startAPIForTest(promqlTest.Storage(), ":9090")

	client, err := api.NewClient(api.Config{Address: "http://127.0.0.1:9090"})
	if err != nil {
		b.Fatalf("Error creating client: %v", err)
	}
	apiClient := v1.NewAPI(client)

	opts := CacheClientOptions{
		StepsPerBucket: 3,
		CachePlugin:    "ccache",
		CacheOptions:   map[string]interface{}{},
	}
	cacheClient, err := NewCacheClient(opts, apiClient)
	if err != nil {
		b.Fatalf("Error creating cacheclient: %v", err)
	}

	// Do an actual test
	ctx := context.TODO()

	for i, test := range cacheTests {
		b.Run(strconv.Itoa(i), func(b *testing.B) {
			b.Run("direct", func(b *testing.B) {
				for x := 0; x < b.N; x++ {
					apiClient.QueryRange(ctx, test.query, test.Range)
				}
			})
			b.Run("cache", func(b *testing.B) {
				for x := 0; x < b.N; x++ {
					cacheClient.QueryRange(ctx, test.query, test.Range)
				}
			})
		})
	}

	// stop server
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	<-stopChan
}
