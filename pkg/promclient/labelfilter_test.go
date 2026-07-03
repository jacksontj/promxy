package promclient

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
)

func newCountAPI(a API) *countAPI {
	return &countAPI{
		API: a,
		callCount: map[string]int{
			"LabelNames":  0,
			"LabelValues": 0,
			"Query":       0,
			"QueryRange":  0,
			"Series":      0,
			"GetValue":    0,
			"Metadata":    0,
		},
	}
}

type countAPI struct {
	API
	callCount map[string]int
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *countAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	s.callCount["LabelNames"]++
	return s.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (s *countAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	s.callCount["LabelValues"]++
	return s.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (s *countAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	s.callCount["Query"]++
	return s.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *countAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	s.callCount["QueryRange"]++
	return s.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (s *countAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	s.callCount["Series"]++
	return s.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *countAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	s.callCount["GetValue"]++
	return s.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *countAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	s.callCount["Metadata"]++
	return s.API.Metadata(ctx, metric, limit)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (s *countAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	s.callCount["QueryExemplars"]++
	return s.API.QueryExemplars(ctx, query, startTime, endTime)
}

func TestLabelFilter(t *testing.T) {
	/*

	   The idea here is that the datasource has the following data:

	   up{filterlabel="a"}
	   up{filterlabel="b"}
	   testmetric{filterlabel="a"}
	   testmetric{filterlabel="b"}

	*/

	stub := &stubAPI{
		// Override the LabelValues endpoint (which is the one that LabelFilter uses to determine its filter)
		labelValues: func(label string) model.LabelValues {
			switch label {
			case "__name__":
				return model.LabelValues{
					"up",
					"testmetric",
				}
			case "filterlabel":
				return model.LabelValues{
					"a",
					"b",
				}
			}
			return model.LabelValues{}
		},
	}

	// Wrap the stub in a counter
	countAPI := newCountAPI(stub)

	// Set up some vars
	ctx := context.TODO() // TODO

	// Create the LabelFilter client
	cfg := &LabelFilterConfig{
		DynamicLabels: []string{"__name__", "filterlabel"},
		StaticLabelsInclude: map[string][]string{
			"__name__": {"staticinclude"},
		},
		StaticLabelsExclude: map[string][]string{
			"__name__": {"up"},
		},
	}

	filterClient, err := NewLabelFilterClient(ctx, countAPI, cfg)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		query     string // query to run
		callCount int    // how many calls expected
	}{
		{query: "notametric"},                            // A metric that definitely doesn't exist
		{query: "testmetric", callCount: 1},              // A metric that does exist
		{query: "staticinclude", callCount: 1},           // A metric that statically exists
		{query: "up"},                                    // A metric that does exist, but we filter out
		{query: `{filterlabel="notavalue"}`},             // A metric that definitely doesn't exist
		{query: `{notalabel="notavalue"}`, callCount: 1}, // A metric that definitely doesn't exist, but isn't filterable
		{query: `{filterlabel="a"}`, callCount: 1},       // A metric that does exist
		{query: `{filterlabel="b"}`, callCount: 1},       // A metric that does exist
	}

	t.Run("Query", func(t *testing.T) {
		for i, test := range tests {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				beforeCount := countAPI.callCount["Query"]
				err := filterClient.Query(ctx, test.query, model.Time(100).Time()).Err()
				if err != nil {
					t.Fatal(err)
				}
				callCount := countAPI.callCount["Query"] - beforeCount
				if test.callCount != callCount {
					t.Fatalf("mismatch in callCount when running %s expected=%d actual=%d", test.query, test.callCount, callCount)
				}
			})
		}
	})

	t.Run("QueryRange", func(t *testing.T) {
		for i, test := range tests {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				beforeCount := countAPI.callCount["QueryRange"]
				err := filterClient.QueryRange(ctx, test.query, v1.Range{Start: model.Time(0).Time(), End: model.Time(100).Time(), Step: time.Millisecond}).Err()
				if err != nil {
					t.Fatal(err)
				}
				callCount := countAPI.callCount["QueryRange"] - beforeCount
				if test.callCount != callCount {
					t.Fatalf("mismatch in callCount when running %s expected=%d actual=%d", test.query, test.callCount, callCount)
				}
			})
		}
	})

	t.Run("Series", func(t *testing.T) {
		for i, test := range tests {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				beforeCount := countAPI.callCount["Series"]
				_, _, err := filterClient.Series(ctx, []string{test.query}, model.Time(0).Time(), model.Time(100).Time())
				if err != nil {
					t.Fatal(err)
				}
				callCount := countAPI.callCount["Series"] - beforeCount
				if test.callCount != callCount {
					t.Fatalf("mismatch in callCount when running %s expected=%d actual=%d", test.query, test.callCount, callCount)
				}
			})
		}
	})

	t.Run("GetValue", func(t *testing.T) {
		for i, test := range tests {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				beforeCount := countAPI.callCount["GetValue"]

				// TODO: convert query to matchers
				matchers, err := parser.ParseMetricSelector(test.query)
				if err != nil {
					t.Fatal(err)
				}

				err = filterClient.GetValue(ctx, model.Time(0).Time(), model.Time(100).Time(), matchers).Err()
				if err != nil {
					t.Fatal(err)
				}
				callCount := countAPI.callCount["GetValue"] - beforeCount
				if test.callCount != callCount {
					t.Fatalf("mismatch in callCount when running %s expected=%d actual=%d", test.query, test.callCount, callCount)
				}
			})
		}
	})

	t.Run("Metadata", func(t *testing.T) {
		tests := []struct {
			metric    string // query to run
			callCount int    // how many calls expected
		}{
			{metric: "notametric"},               // A metric that definitely doesn't exist
			{metric: "testmetric", callCount: 1}, // A metric that does exist
			{metric: "up"},                       // A metric that does exist, but we filter out
		}

		for i, test := range tests {
			t.Run(strconv.Itoa(i), func(t *testing.T) {
				beforeCount := countAPI.callCount["Metadata"]
				_, err := filterClient.Metadata(ctx, test.metric, "")
				if err != nil {
					t.Fatal(err)
				}
				callCount := countAPI.callCount["Metadata"] - beforeCount
				if test.callCount != callCount {
					t.Fatalf("mismatch in callCount when running %s expected=%d actual=%d", test.metric, test.callCount, callCount)
				}
			})
		}
	})

}

// flakyLabelValuesAPI lets a test toggle whether the downstream LabelValues call
// (the one the label_filter sync uses) succeeds, and counts Query passthroughs.
// All state is mutex-guarded so it is safe to poke from a test while the
// LabelFilterClient background sync goroutine is running.
type flakyLabelValuesAPI struct {
	*stubAPI

	mu         sync.Mutex
	fail       bool
	values     model.LabelValues
	queryCount int
}

func (s *flakyLabelValuesAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return nil, nil, fmt.Errorf("downstream unavailable")
	}
	return s.values, nil, nil
}

func (s *flakyLabelValuesAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	s.mu.Lock()
	s.queryCount++
	s.mu.Unlock()
	return s.stubAPI.Query(ctx, query, ts)
}

func (s *flakyLabelValuesAPI) setFail(b bool) {
	s.mu.Lock()
	s.fail = b
	s.mu.Unlock()
}

func (s *flakyLabelValuesAPI) getQueryCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.queryCount
}

func TestLabelFilterConfigOnSyncErrorValidate(t *testing.T) {
	// Empty defaults to abort.
	c := &LabelFilterConfig{}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.OnSyncError != LabelFilterOnSyncErrorAbort {
		t.Fatalf("expected default on_sync_error=abort, got %q", c.OnSyncError)
	}

	// Explicit valid values are accepted.
	for _, v := range []LabelFilterOnSyncError{LabelFilterOnSyncErrorAbort, LabelFilterOnSyncErrorOpen, LabelFilterOnSyncErrorClosed} {
		c := &LabelFilterConfig{OnSyncError: v}
		if err := c.Validate(); err != nil {
			t.Fatalf("unexpected error for on_sync_error=%q: %v", v, err)
		}
	}

	// Unknown values are rejected.
	c = &LabelFilterConfig{OnSyncError: "bogus"}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error for invalid on_sync_error")
	}
}

func TestLabelFilterOnSyncError(t *testing.T) {
	// abort (the default) surfaces the initial sync error, which blocks startup.
	t.Run("abort", func(t *testing.T) {
		api := &flakyLabelValuesAPI{stubAPI: &stubAPI{}, fail: true}
		cfg := &LabelFilterConfig{DynamicLabels: []string{"__name__"}}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		if _, err := NewLabelFilterClient(context.Background(), api, cfg); err == nil {
			t.Fatal("expected NewLabelFilterClient to fail when the initial sync errors under on_sync_error=abort")
		}
	})

	// open lets startup proceed and sends queries downstream (unfiltered) until synced.
	t.Run("open", func(t *testing.T) {
		api := &flakyLabelValuesAPI{stubAPI: &stubAPI{}, fail: true}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := &LabelFilterConfig{DynamicLabels: []string{"__name__"}, OnSyncError: LabelFilterOnSyncErrorOpen}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		c, err := NewLabelFilterClient(ctx, api, cfg)
		if err != nil {
			t.Fatalf("expected startup to proceed under on_sync_error=open, got: %v", err)
		}
		if c.LabelFilter() != nil {
			t.Fatal("expected filter to be unloaded after a failed initial sync")
		}
		// Unloaded + open => query is passed straight through to the downstream.
		before := api.getQueryCount()
		if err := c.Query(ctx, "anymetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before+1 {
			t.Fatalf("expected query to pass through while unloaded (open), downstream calls before=%d after=%d", before, got)
		}
	})

	// closed lets startup proceed but filters out everything until the first
	// successful sync, then recovers and filters normally.
	t.Run("closed_then_recovers", func(t *testing.T) {
		api := &flakyLabelValuesAPI{stubAPI: &stubAPI{}, fail: true, values: model.LabelValues{"knownmetric"}}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		cfg := &LabelFilterConfig{
			DynamicLabels: []string{"__name__"},
			OnSyncError:   LabelFilterOnSyncErrorClosed,
			SyncInterval:  20 * time.Millisecond,
		}
		if err := cfg.Validate(); err != nil {
			t.Fatal(err)
		}
		c, err := NewLabelFilterClient(ctx, api, cfg)
		if err != nil {
			t.Fatalf("expected startup to proceed under on_sync_error=closed, got: %v", err)
		}

		// While unloaded, everything is filtered out (target treated as down).
		before := api.getQueryCount()
		if err := c.Query(ctx, "knownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before {
			t.Fatalf("expected query to be blocked while unloaded (closed), but downstream was called: before=%d after=%d", before, got)
		}

		// Recover the downstream and wait for a background sync to load the filter.
		api.setFail(false)
		deadline := time.Now().Add(2 * time.Second)
		for c.LabelFilter() == nil && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if c.LabelFilter() == nil {
			t.Fatal("filter never synced after the downstream recovered")
		}

		// Now filtering behaves normally: known metric passes, unknown is filtered.
		before = api.getQueryCount()
		if err := c.Query(ctx, "knownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before+1 {
			t.Fatalf("expected knownmetric to pass through after sync: before=%d after=%d", before, got)
		}
		before = api.getQueryCount()
		if err := c.Query(ctx, "unknownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before {
			t.Fatalf("expected unknownmetric to be filtered after sync: before=%d after=%d", before, got)
		}
	})
}
