package proxystorage

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promclient"
)

type stubAPI struct {
	queries []string
}

func (a *stubAPI) getQueries() []string {
	ret := a.queries
	a.queries = []string{}
	return ret
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (a *stubAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}

// LabelValues performs a query for the values of the given label.
func (a *stubAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}

// Query performs a query for the given time.
func (a *stubAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	a.queries = append(a.queries, query+" @ "+model.TimeFromUnixNano(ts.UnixNano()).String())
	return promclient.ModelValueToSeriesSet(model.Vector{}, nil, nil)
}

// QueryRange performs a query for the given range.
func (a *stubAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	from := fmt.Sprintf("%s to %s step %s",
		model.TimeFromUnix(r.Start.Unix()).String(),
		model.TimeFromUnix(r.End.Unix()).String(),
		r.Step.String(),
	)
	a.queries = append(a.queries, query+" @ "+from)
	return promclient.ModelValueToSeriesSet(model.Matrix{}, nil, nil)
}

// Series finds series by label matchers.
func (a *stubAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *stubAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return promclient.ModelValueToSeriesSet(model.Vector{}, nil, nil)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (a *stubAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, nil
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (a *stubAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}

func TestNodeReplacer(t *testing.T) {
	logrus.SetLevel(logrus.DebugLevel)

	tests := []struct {
		in      string   // Expr for NodeReplacer
		queries []string // Queries sent to downstream
		out     string   // Returned Expr (tree replaced)
	}{
		// AggregateExprs
		// Simple no-op
		{
			in:  "sum(foo)",
			out: "sum()",
			queries: []string{
				"sum(foo) @ 10000",
			},
		},
		// Simple no-op
		{
			in:  "sum(foo offset 1h)",
			out: "sum( offset 1h)",
			queries: []string{
				"sum(foo) @ 6400",
			},
		},
		// test that average is converted to sum / count
		{
			in:  "avg(foo)",
			out: "sum(foo) / count(foo)",
		},
		// Sum of a call (irate) should be filled in as just a sum
		{
			in:  "sum(irate(foo[1m]))",
			out: "sum()",
			queries: []string{
				"sum(irate(foo[1m])) @ 10000",
			},
		},
		// Average of a call (irate) should be replaced to a sum and count
		{
			in:  "avg(irate(foo[1m]))",
			out: "sum(irate(foo[1m])) / count(irate(foo[1m]))",
		},
		// count_values is both a query downstream *AND* a tree replacement
		{
			in:  "count_values(\"label\", foo)",
			out: "sum by (label) ()",
			queries: []string{
				"count_values(\"label\", foo) @ 10000",
			},
		},

		// Call
		// basic call; we expect a full replacement
		{
			in:  "irate(foo[1m])",
			out: "",
			queries: []string{
				"irate(foo[1m]) @ 10000",
			},
		},
		// Scalar method; expect replacement
		{
			in:  "scalar(foo{})",
			out: "scalar()",
			queries: []string{
				"scalar(foo) @ 10000",
			},
		},
		// Sort method; expect replacement AND rewrite
		{
			in:  "sort(foo)",
			out: "sort()",
			queries: []string{
				"sort(foo) @ 10000",
			},
		},

		// VectorSelector
		{
			in:  "foo{}",
			out: "foo",
			queries: []string{
				"foo @ 10000",
			},
		},

		// SubqueryExpr
		// Expect a full replacement and a query downstream
		{
			in:  "rate(http_requests_total[5m])[30m:1m]",
			out: "[30m:1m]",
			queries: []string{
				"rate(http_requests_total[5m]) @ 8220 to 10000 step 1m0s",
			},
		},
		{
			in:  "sum(foo{} offset 30m)[1h:5m]",
			out: "sum( offset 30m)[1h:5m]",
			queries: []string{
				"sum(foo) @ 4800 to 8200 step 5m0s",
			},
		},

		// BinaryExpr
		// If it is a VectorSelector + scalar -- we expect it to work
		{
			in:  "foo{} > 1",
			out: "",
			queries: []string{
				"foo > 1 @ 10000",
			},
		},
		{
			in:  "1 > foo{}",
			out: "",
			queries: []string{
				"1 > foo @ 10000",
			},
		},
		// If it is SOME AggregateExpr and a scalar -- also valid; in those cases
		// we expect to re-do the AggregateExpr but have replaced the VectorSelector with the query
		{
			in:  "min(foo{}) > 1",
			out: "min()",
			queries: []string{
				"min(foo) > 1 @ 10000",
			},
		},
		{
			in:  "max(foo{}) > 1",
			out: "max()",
			queries: []string{
				"max(foo) > 1 @ 10000",
			},
		},
		{
			in:  "topk(5, foo{}) > 1",
			out: "topk(5, )",
			queries: []string{
				"topk(5, foo) > 1 @ 10000",
			},
		},
		{
			in:  "bottomk(5, foo{}) > 1",
			out: "bottomk(5, )",
			queries: []string{
				"bottomk(5, foo) > 1 @ 10000",
			},
		},

		{
			in:  "min(foo{}) * 1000",
			out: "min()",
			queries: []string{
				"min(foo) * 1000 @ 10000",
			},
		},
		{
			in:  "max(foo{}) * 1000",
			out: "max()",
			queries: []string{
				"max(foo) * 1000 @ 10000",
			},
		},
		{
			in:  "topk(5, foo{}) * 1000",
			out: "topk(5, )",
			queries: []string{
				"topk(5, foo) * 1000 @ 10000",
			},
		},
		{
			in:  "bottomk(5, foo{}) * 1000",
			out: "bottomk(5, )",
			queries: []string{
				"bottomk(5, foo) * 1000 @ 10000",
			},
		},

		// Check that some others do nothing
		{
			in: "avg(foo) > 1",
		},

		// Test current no-ops
		{
			in: "foo[5m]",
		},
		{
			in: "quantile(0.95, foo)",
		},
		{
			in: "stddev(foo)",
		},
		{
			in: "stdvar(foo)",
		},

		// @ modifier — pushdown contract.
		//
		// AggregateExpr SUM/MIN/MAX/TOPK/BOTTOMK/GROUP carries the @ modifier
		// down to the wire so the downstream resolves it. The synthesized
		// replacement has no offset and the request range is NOT shifted.
		{
			in:  "sum(foo @ 100)",
			out: "sum()",
			queries: []string{
				"sum(foo @ 100.000) @ 10000",
			},
		},
		// @ + offset must travel together — stripping the offset would silently
		// change the lookup time at the downstream.
		{
			in:  "sum(foo @ 100 offset 50s)",
			out: "sum()",
			queries: []string{
				"sum(foo @ 100.000 offset 50s) @ 10000",
			},
		},
		{
			in:  "min(foo @ 100)",
			out: "min()",
			queries: []string{
				"min(foo @ 100.000) @ 10000",
			},
		},
		// COUNT pushes down under @ — same shape as SUM/MIN/MAX, with the
		// in-place n.Op = SUM rewrite still applied so the engine
		// re-aggregates the per-shard counts.
		{
			in:  "count(foo @ 100)",
			out: "sum()",
			queries: []string{
				"count(foo @ 100.000) @ 10000",
			},
		},
		// Call (rate, irate, …) under @ pushes down. PromAPIV1's custom
		// HTTP path parses both `warnings` and `infos` from the response,
		// and WarningsConvert re-wraps them with the right sentinel so
		// info-level annotations survive.
		{
			in:  "rate(foo[1m] @ 100)",
			out: "",
			queries: []string{
				"rate(foo[1m] @ 100.000) @ 10000",
			},
		},
		{
			in:  "irate(foo[1m] @ 100)",
			out: "",
			queries: []string{
				"irate(foo[1m] @ 100.000) @ 10000",
			},
		},
		// Bare VectorSelector with @ pushes down. The downstream resolves
		// @ T (and any offset) when evaluating the selector; we synthesize
		// a flat VectorSelector whose samples sit at the request
		// timestamps so the engine looks them up by ts directly instead
		// of re-applying the @ pin and offset to a sample set that's
		// already step-invariant. We preserve Name and LabelMatchers on
		// the synthesized node so functions like absent() that derive
		// their output labels from the selector's matchers
		// (createLabelsForAbsentFunction) still see them.
		{
			in:  "foo @ 100",
			out: "foo",
			queries: []string{
				"foo @ 100.000 @ 10000",
			},
		},
		// BinaryExpr with an aggregate-with-@ + literal: still a valid
		// pushdown shape since the synthesized aggregate replacement
		// carries the @-bearing string.
		{
			in:  "min(foo @ 100) > 1",
			out: "min()",
			queries: []string{
				"min(foo @ 100.000) > 1 @ 10000",
			},
		},
	}

	api := &stubAPI{}
	ps := &ProxyStorage{}
	ps.state.Store(&proxyStorageState{
		client: api,
	})

	now := time.Unix(10000, 0)

	for i, test := range tests {
		t.Run(strconv.Itoa(i)+": "+test.in, func(t *testing.T) {
			expr, err := parser.ParseExpr(test.in)
			if err != nil {
				t.Fatal(err)
			}

			stmt := &parser.EvalStmt{
				Expr:  expr,
				Start: now,
				End:   now,
			}

			node, err := ps.NodeReplacer(context.TODO(), stmt, expr, nil)
			if err != nil {
				t.Fatal(err)
			}
			if node == nil {
				if test.out != "" {
					t.Fatalf("nil return with out expected")
				}
				return
			}

			// If we didn't define them; lets set empty (for nicer diffs)
			if test.queries == nil {
				test.queries = []string{}
			}
			queries := api.getQueries()
			if !cmp.Equal(test.queries, queries) {
				t.Fatalf("mismatch in queries: \n%s", cmp.Diff(test.queries, queries))
			}

			if node.String() != test.out {
				t.Fatalf("mismatch expected=%s actual=%s", test.out, node.String())
			}
		})
	}

	//func (p *ProxyStorage) NodeReplacer(ctx context.Context, s *parser.EvalStmt, node parser.Node, path []parser.Node) (parser.Node, error) {

}

func TestVectorToStepMatrix(t *testing.T) {
	// Instant-query result: one sample per series at the @ time. The input
	// timestamps are irrelevant — vectorToStepMatrix replicates each value
	// across the step grid.
	vec := promapi.NewSeriesSet([]storage.Series{
		promapi.NewSeries(labels.FromStrings("__name__", "foo", "instance", "a"),
			[]chunks.Sample{promapi.FloatSample(4000, 1.5)}),
		promapi.NewSeries(labels.FromStrings("__name__", "foo", "instance", "b"),
			[]chunks.Sample{promapi.FloatSample(4000, 2.5)}),
	}, nil, nil)

	// Range [-59.2s, 60.8s] step 60s → 3 steps at -59200ms, 800ms, 60800ms.
	// The synthesized series MUST place samples exactly at those step times
	// (not at the @ time) so the engine's step-by-step lookup finds the
	// pinned value at each step within its LookbackDelta window.
	start := time.Unix(0, 800*int64(time.Millisecond)).Add(-time.Minute)
	end := time.Unix(0, 800*int64(time.Millisecond)).Add(time.Minute)

	type sample struct {
		t int64
		v float64
	}
	read := func(ss storage.SeriesSet) [][]sample {
		var got [][]sample
		for ss.Next() {
			var pts []sample
			it := ss.At().Iterator(nil)
			for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
				ts, v := it.At()
				pts = append(pts, sample{ts, v})
			}
			if err := it.Err(); err != nil {
				t.Fatalf("iterator error: %v", err)
			}
			got = append(got, pts)
		}
		return got
	}

	mat := read(vectorToStepMatrix(vec, start, end, time.Minute))
	if len(mat) != 2 {
		t.Fatalf("expected 2 streams, got %d", len(mat))
	}
	for i, stream := range mat {
		if len(stream) != 3 {
			t.Fatalf("stream %d: expected 3 samples, got %d", i, len(stream))
		}
		wantTs := []int64{-59200, 800, 60800}
		wantVal := []float64{1.5, 2.5}[i]
		for j, sp := range stream {
			if sp.t != wantTs[j] {
				t.Errorf("stream %d sample %d: got ts %d, want %d", i, j, sp.t, wantTs[j])
			}
			if sp.v != wantVal {
				t.Errorf("stream %d sample %d: got value %v, want %v", i, j, sp.v, wantVal)
			}
		}
	}

	// Step ≤ 0 → empty set (defensive: caller gates on s.Interval > 0).
	vec2 := promapi.NewSeriesSet([]storage.Series{
		promapi.NewSeries(labels.FromStrings("__name__", "foo"),
			[]chunks.Sample{promapi.FloatSample(4000, 1.5)}),
	}, nil, nil)
	if got := read(vectorToStepMatrix(vec2, start, end, 0)); len(got) != 0 {
		t.Errorf("step=0: expected empty set, got %v", got)
	}
}
