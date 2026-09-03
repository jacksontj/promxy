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
	mu        sync.Mutex
	callCount map[string]int
}

func (s *countAPI) count(name string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.callCount[name]
}

func (s *countAPI) inc(name string) {
	s.mu.Lock()
	s.callCount[name]++
	s.mu.Unlock()
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *countAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	s.inc("LabelNames")
	return s.API.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (s *countAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	s.inc("LabelValues")
	return s.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (s *countAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	s.inc("Query")
	return s.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *countAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	s.inc("QueryRange")
	return s.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (s *countAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	s.inc("Series")
	return s.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *countAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	s.inc("GetValue")
	return s.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *countAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	s.inc("Metadata")
	return s.API.Metadata(ctx, metric, limit)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (s *countAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	s.inc("QueryExemplars")
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

	// recover when the first sync succeeded but later on the target times out
	t.Run("closed_then_relapses", func(t *testing.T) {
		api := &flakyLabelValuesAPI{stubAPI: &stubAPI{}, fail: false, values: model.LabelValues{"knownmetric"}}
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
			t.Fatalf("expected initial sync to succeed, got: %v", err)
		}
		if c.LabelFilter() == nil {
			t.Fatal("expected filter to be loaded after a successful initial sync")
		}

		// Sanity check: queries pass through while healthy
		before := api.getQueryCount()
		if err := c.Query(ctx, "knownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before+1 {
			t.Fatalf("expected knownmetric to pass through while synced: before=%d after=%d", before, got)
		}

		// Now the downstream starts failing (e.g. it starts timing out)
		api.setFail(true)
		deadline := time.Now().Add(2 * time.Second)
		for !c.blocked() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if !c.blocked() {
			t.Fatal("expected client to become blocked after a later sync failed")
		}

		// The stale (previously-successful) filter is still cached
		if c.LabelFilter() == nil {
			t.Fatal("expected the stale filter to remain cached, not cleared, on a later sync failure")
		}

		// Even a known-good metric must now be blocked
		before = api.getQueryCount()
		if err := c.Query(ctx, "knownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before {
			t.Fatalf("expected query to be blocked after sync relapsed, but downstream was called: before=%d after=%d", before, got)
		}

		// The target recovers again: once a later sync succeeds, the client
		// must unblock
		api.setFail(false)
		deadline = time.Now().Add(2 * time.Second)
		for c.blocked() && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if c.blocked() {
			t.Fatal("expected client to unblock after a later sync succeeded again")
		}

		before = api.getQueryCount()
		if err := c.Query(ctx, "knownmetric", time.Now()).Err(); err != nil {
			t.Fatal(err)
		}
		if got := api.getQueryCount(); got != before+1 {
			t.Fatalf("expected knownmetric to pass through again after recovering from relapse: before=%d after=%d", before, got)
		}
	})
}

// referenceFilterLabelMatchers is the naive (pre-optimization) implementation of
// FilterLabelMatchers; it simply calls matcher.Matches() over every value in the
// filter's value set. The optimized implementation must agree with this for
// every input.
func referenceFilterLabelMatchers(filter map[string]map[string]struct{}, matcher *labels.Matcher) bool {
	for labelName, labelFilter := range filter {
		if matcher.Name == labelName {
			match := false
			// Check that there is a match somewhere!
			for v := range labelFilter {
				if matcher.Matches(v) {
					match = true
					break
				}
			}
			if !match {
				return match
			}
		}
	}

	return true
}

func TestFilterLabelMatchers(t *testing.T) {
	tests := []struct {
		name    string
		filter  map[string]map[string]struct{}
		matcher *labels.Matcher
		expect  bool
	}{
		{
			name:    "nil filter",
			filter:  nil,
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "label absent from filter",
			filter:  map[string]map[string]struct{}{"job": {"a": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "label absent from filter, notequal",
			filter:  map[string]map[string]struct{}{"job": {"a": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "empty value set, equal",
			filter:  map[string]map[string]struct{}{"__name__": {}},
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", "a"),
			expect:  false,
		},
		{
			name:    "empty value set, notequal",
			filter:  map[string]map[string]struct{}{"__name__": {}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "a"),
			expect:  false,
		},
		{
			name:    "empty value set, regexp matching everything",
			filter:  map[string]map[string]struct{}{"__name__": {}},
			matcher: labels.MustNewMatcher(labels.MatchRegexp, "__name__", ".*"),
			expect:  false,
		},
		{
			name:    "equal hit",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}, "b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "equal miss",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}, "b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", "z"),
			expect:  false,
		},
		{
			name:    "single element set, notequal that same value",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "a"),
			expect:  false,
		},
		{
			name:    "single element set, notequal some other value",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "z"),
			expect:  true,
		},
		{
			name:    "multi element set, notequal a member",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}, "b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "empty string value, equal",
			filter:  map[string]map[string]struct{}{"__name__": {"": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchEqual, "__name__", ""),
			expect:  true,
		},
		{
			name:    "empty string value, notequal empty string",
			filter:  map[string]map[string]struct{}{"__name__": {"": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", ""),
			expect:  false,
		},
		{
			name:    "empty string value, notequal something else",
			filter:  map[string]map[string]struct{}{"__name__": {"": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotEqual, "__name__", "a"),
			expect:  true,
		},
		{
			name:    "regexp setmatches hit",
			filter:  map[string]map[string]struct{}{"__name__": {"c": struct{}{}, "d": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchRegexp, "__name__", "a|b|c"),
			expect:  true,
		},
		{
			name:    "regexp setmatches miss",
			filter:  map[string]map[string]struct{}{"__name__": {"x": struct{}{}, "y": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchRegexp, "__name__", "a|b|c"),
			expect:  false,
		},
		{
			name:    "regexp no match",
			filter:  map[string]map[string]struct{}{"__name__": {"foo_a": struct{}{}, "foo_b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchRegexp, "__name__", "bar_.+"),
			expect:  false,
		},
		{
			name:    "regexp match",
			filter:  map[string]map[string]struct{}{"__name__": {"foo_a": struct{}{}, "foo_b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchRegexp, "__name__", "foo_.+"),
			expect:  true,
		},
		{
			name:    "notregexp setmatches covering the whole set",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}, "b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotRegexp, "__name__", "a|b|c"),
			expect:  false,
		},
		{
			name:    "notregexp setmatches leaving something",
			filter:  map[string]map[string]struct{}{"__name__": {"a": struct{}{}, "z": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotRegexp, "__name__", "a|b|c"),
			expect:  true,
		},
		{
			name:    "notregexp genuine regex",
			filter:  map[string]map[string]struct{}{"__name__": {"foo_a": struct{}{}, "foo_b": struct{}{}}},
			matcher: labels.MustNewMatcher(labels.MatchNotRegexp, "__name__", "foo_.+"),
			expect:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := FilterLabelMatchers(test.filter, test.matcher); got != test.expect {
				t.Fatalf("expected %v got %v", test.expect, got)
			}
			// And it must always agree with the naive implementation
			if got, want := FilterLabelMatchers(test.filter, test.matcher), referenceFilterLabelMatchers(test.filter, test.matcher); got != want {
				t.Fatalf("disagrees with reference implementation: got %v want %v", got, want)
			}
		})
	}
}

// TestFilterLabelMatchersMatchesReference exhaustively compares the optimized
// implementation against the naive one across a cross-product of filters and
// matchers of all four matcher types.
func TestFilterLabelMatchersMatchesReference(t *testing.T) {
	newSet := func(vals ...string) map[string]struct{} {
		s := make(map[string]struct{}, len(vals))
		for _, v := range vals {
			s[v] = struct{}{}
		}
		return s
	}

	filters := map[string]map[string]map[string]struct{}{
		"nil":                nil,
		"empty":              {},
		"other label only":   {"job": newSet("a", "b")},
		"empty set":          {"__name__": newSet()},
		"single a":           {"__name__": newSet("a")},
		"single empty":       {"__name__": newSet("")},
		"single z":           {"__name__": newSet("z")},
		"multi abc":          {"__name__": newSet("a", "b", "c")},
		"multi with empty":   {"__name__": newSet("", "a")},
		"multi foo":          {"__name__": newSet("foo_a", "foo_b")},
		"multi mixed":        {"__name__": newSet("a", "foo_a", "")},
		"multi plus other":   {"__name__": newSet("a", "b"), "job": newSet("x")},
		"disjoint from case": {"__name__": newSet("x", "y", "z")},
	}

	values := []string{"", "a", "b", "z", "foo_a", "a|b"}
	regexes := []string{"", ".*", ".+", "a", "z", "a|b|c", "(a|z)", "[ab]", "foo_.+", "bar_.+", "a.*", "^$"}

	for filterName, filter := range filters {
		for _, v := range values {
			for _, mt := range []labels.MatchType{labels.MatchEqual, labels.MatchNotEqual} {
				matcher := labels.MustNewMatcher(mt, "__name__", v)
				t.Run(fmt.Sprintf("%s/%s", filterName, matcher.String()), func(t *testing.T) {
					got := FilterLabelMatchers(filter, matcher)
					want := referenceFilterLabelMatchers(filter, matcher)
					if got != want {
						t.Fatalf("got %v want %v", got, want)
					}
				})
			}
		}
		for _, re := range regexes {
			for _, mt := range []labels.MatchType{labels.MatchRegexp, labels.MatchNotRegexp} {
				matcher, err := labels.NewMatcher(mt, "__name__", re)
				if err != nil {
					t.Fatalf("error building matcher for %q: %v", re, err)
				}
				t.Run(fmt.Sprintf("%s/%s", filterName, matcher.String()), func(t *testing.T) {
					got := FilterLabelMatchers(filter, matcher)
					want := referenceFilterLabelMatchers(filter, matcher)
					if got != want {
						t.Fatalf("got %v want %v", got, want)
					}
				})
			}
		}
	}
}

func BenchmarkFilterLabelMatchers(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		names := make(map[string]struct{}, size)
		for i := 0; i < size; i++ {
			names["metric_"+strconv.Itoa(i)] = struct{}{}
		}
		filter := map[string]map[string]struct{}{labels.MetricName: names}

		// The last name added; a value which is definitely in the filter
		present := "metric_" + strconv.Itoa(size-1)

		matchers := []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, present),
			labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "absent_metric"),
			labels.MustNewMatcher(labels.MatchNotEqual, labels.MetricName, present),
			labels.MustNewMatcher(labels.MatchRegexp, labels.MetricName, "absent_a|absent_b|absent_c"),
			labels.MustNewMatcher(labels.MatchRegexp, labels.MetricName, "absent_a|absent_b|"+present),
			labels.MustNewMatcher(labels.MatchNotRegexp, labels.MetricName, "absent_a|absent_b|absent_c"),
			labels.MustNewMatcher(labels.MatchRegexp, labels.MetricName, "absent_.+"),
		}
		caseNames := []string{
			"equal_hit",
			"equal_miss",
			"notequal_hit",
			"regexp_set_miss",
			"regexp_set_hit",
			"notregexp_set_hit",
			"regexp_scan_miss",
		}

		for i, matcher := range matchers {
			b.Run(fmt.Sprintf("size=%d/%s", size, caseNames[i]), func(b *testing.B) {
				b.ReportAllocs()
				for n := 0; n < b.N; n++ {
					FilterLabelMatchers(filter, matcher)
				}
			})
		}
	}
}
