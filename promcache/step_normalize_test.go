package promcache

import (
	"context"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
)

func allSameValue(a []model.Value) bool {
	for i := 1; i < len(a); i++ {
		if !reflect.DeepEqual(a[i], a[0]) {
			return false
		}
	}
	return true
}

func TestStepNormalize(t *testing.T) {
	normalizeTests := []struct {
		query  string
		ranges []v1.Range
	}{
		{
			"http_requests",
			[]v1.Range{
				v1.Range{Start: zeroTime.Add(0 * time.Second), End: zeroTime.Add(100 * time.Second), Step: time.Second * 25},
				v1.Range{Start: zeroTime.Add(1 * time.Second), End: zeroTime.Add(101 * time.Second), Step: time.Second * 25},
				v1.Range{Start: zeroTime.Add(24 * time.Second), End: zeroTime.Add(124 * time.Second), Step: time.Second * 25},
			},
		},
	}

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
	cache, err := New("ccache", map[string]interface{}{})
	if err != nil {
		t.Fatalf("Error creating cache: %v", err)
	}
	cacheClient := NewCacheClient(countClient, cache)
	normalClient := StepNormalizingClient{cacheClient}

	// Do an actual test
	ctx := context.TODO()

	for i, test := range normalizeTests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			vals := make([]model.Value, len(test.ranges))
			for x, r := range test.ranges {
				v, err := normalClient.QueryRange(ctx, test.query, r)
				if err != nil {
					t.Fatalf("Error running query: %v", err)
				}
				vals[x] = v
			}

			if !allSameValue(vals) {
				t.Fatalf("mismatch in returns: %v", vals)
			}

		})
	}

	// stop server
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
	<-stopChan
}
