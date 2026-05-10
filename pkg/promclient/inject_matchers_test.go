package promclient

import (
	"context"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
)

// recordingAPI captures the query/matchers it is called with so tests can assert what
// would have been sent to the downstream.
type recordingAPI struct {
	query   string
	matches []string
	getVal  []*labels.Matcher
}

func (a *recordingAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	a.matches = matchers
	return nil, nil, nil
}

func (a *recordingAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	a.matches = matchers
	return nil, nil, nil
}

func (a *recordingAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	a.query = query
	return storage.EmptySeriesSet()
}

func (a *recordingAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	a.query = query
	return storage.EmptySeriesSet()
}

func (a *recordingAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	a.matches = matches
	return nil, nil, nil
}

func (a *recordingAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	a.getVal = matchers
	return storage.EmptySeriesSet()
}

func (a *recordingAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, nil
}

func (a *recordingAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	a.query = query
	return nil, nil
}

// mustMatchers parses a comma-separated set of bare matchers into label matchers.
func mustMatchers(t *testing.T, s string) []*labels.Matcher {
	t.Helper()
	matchers, err := parser.ParseMetricSelector("{" + s + "}")
	if err != nil {
		t.Fatalf("error parsing matchers %q: %v", s, err)
	}
	return matchers
}

func TestInjectMatchersQuery(t *testing.T) {
	tests := []struct {
		name     string
		inject   string
		query    string
		expected string
	}{
		{
			name:     "aggregation without the injected label",
			inject:   `cluster="A"`,
			query:    `count(up) by (job)`,
			expected: `count by (job) (up{cluster="A"})`,
		},
		{
			name:     "selector already referencing a different label",
			inject:   `cluster="A"`,
			query:    `up{job="foo"}`,
			expected: `up{cluster="A",job="foo"}`,
		},
		{
			name:     "matrix selector inside a function",
			inject:   `cluster="A"`,
			query:    `rate(http_requests_total[5m])`,
			expected: `rate(http_requests_total{cluster="A"}[5m])`,
		},
		{
			name:     "binary expression injects into both sides",
			inject:   `cluster="A"`,
			query:    `up / on() node_up`,
			expected: `up{cluster="A"} / on () node_up{cluster="A"}`,
		},
		{
			name:     "multiple matchers",
			inject:   `cluster="A",region=~"us-.*"`,
			query:    `up`,
			expected: `up{cluster="A",region=~"us-.*"}`,
		},
		{
			name:     "idempotent when matcher already present",
			inject:   `cluster="A"`,
			query:    `up{cluster="A"}`,
			expected: `up{cluster="A"}`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := &recordingAPI{}
			c, err := NewInjectMatchersClient(rec, mustMatchers(t, test.inject))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			if err := c.Query(context.TODO(), test.query, time.Time{}).Err(); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if rec.query != test.expected {
				t.Fatalf("Query mismatch\nexpected=%s\nactual=%s", test.expected, rec.query)
			}

			// QueryRange should rewrite identically
			rec.query = ""
			if err := c.QueryRange(context.TODO(), test.query, v1.Range{}).Err(); err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if rec.query != test.expected {
				t.Fatalf("QueryRange mismatch\nexpected=%s\nactual=%s", test.expected, rec.query)
			}
		})
	}
}

func TestInjectMatchersMatches(t *testing.T) {
	tests := []struct {
		name     string
		inject   string
		matches  []string
		expected []string
	}{
		{
			name:     "empty matches scopes to the injected matchers",
			inject:   `cluster="A"`,
			matches:  nil,
			expected: []string{`{cluster="A"}`},
		},
		{
			name:     "injects into each provided selector",
			inject:   `cluster="A"`,
			matches:  []string{`up{job="foo"}`, `node_up`},
			expected: []string{`{job="foo",__name__="up",cluster="A"}`, `{__name__="node_up",cluster="A"}`},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			rec := &recordingAPI{}
			c, err := NewInjectMatchersClient(rec, mustMatchers(t, test.inject))
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}

			t.Run("Series", func(t *testing.T) {
				rec.matches = nil
				if _, _, err := c.Series(context.TODO(), test.matches, time.Time{}, time.Time{}); err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				assertMatchesEqual(t, test.expected, rec.matches)
			})

			t.Run("LabelValues", func(t *testing.T) {
				rec.matches = nil
				if _, _, err := c.LabelValues(context.TODO(), "job", test.matches, time.Time{}, time.Time{}); err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				assertMatchesEqual(t, test.expected, rec.matches)
			})

			t.Run("LabelNames", func(t *testing.T) {
				rec.matches = nil
				if _, _, err := c.LabelNames(context.TODO(), test.matches, time.Time{}, time.Time{}); err != nil {
					t.Fatalf("unexpected err: %v", err)
				}
				assertMatchesEqual(t, test.expected, rec.matches)
			})
		})
	}
}

func TestInjectMatchersGetValue(t *testing.T) {
	rec := &recordingAPI{}
	c, err := NewInjectMatchersClient(rec, mustMatchers(t, `cluster="A"`))
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	in := mustMatchers(t, `__name__="up",job="foo"`)
	if err := c.GetValue(context.TODO(), time.Time{}, time.Time{}, in).Err(); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}

	got, err := selectorString(rec.getVal)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	expected := `{__name__="up",job="foo",cluster="A"}`
	if got != expected {
		t.Fatalf("GetValue mismatch\nexpected=%s\nactual=%s", expected, got)
	}
}

func assertMatchesEqual(t *testing.T, expected, actual []string) {
	t.Helper()
	if len(expected) != len(actual) {
		t.Fatalf("len mismatch\nexpected=%v\nactual=%v", expected, actual)
	}
	for i := range expected {
		if expected[i] != actual[i] {
			t.Fatalf("mismatch at %d\nexpected=%v\nactual=%v", i, expected, actual)
		}
	}
}

func selectorString(matchers []*labels.Matcher) (string, error) {
	ret := "{"
	for i, m := range matchers {
		if i > 0 {
			ret += ","
		}
		ret += m.String()
	}
	return ret + "}", nil
}
