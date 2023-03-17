package promclient

import (
	"context"
	"strconv"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
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
func (s *countAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	s.callCount["Query"]++
	return s.API.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *countAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	s.callCount["QueryRange"]++
	return s.API.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (s *countAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	s.callCount["Series"]++
	return s.API.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *countAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	s.callCount["GetValue"]++
	return s.API.GetValue(ctx, start, end, matchers)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *countAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	s.callCount["Metadata"]++
	return s.API.Metadata(ctx, metric, limit)
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
				_, _, err := filterClient.Query(ctx, test.query, model.Time(100).Time())
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
				_, _, err := filterClient.QueryRange(ctx, test.query, v1.Range{Start: model.Time(0).Time(), End: model.Time(100).Time(), Step: time.Millisecond})
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

				_, _, err = filterClient.GetValue(ctx, model.Time(0).Time(), model.Time(100).Time(), matchers)
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
