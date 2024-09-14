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

	"github.com/sirupsen/logrus"
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
func (a *stubAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	a.queries = append(a.queries, query+" @ "+model.TimeFromUnixNano(ts.UnixNano()).String())
	return model.Vector{}, nil, nil
}

// QueryRange performs a query for the given range.
func (a *stubAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	from := fmt.Sprintf("%s to %s step %s",
		model.TimeFromUnix(r.Start.Unix()).String(),
		model.TimeFromUnix(r.End.Unix()).String(),
		r.Step.String(),
	)
	a.queries = append(a.queries, query+" @ "+from)
	return model.Matrix{}, nil, nil
}

// Series finds series by label matchers.
func (a *stubAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *stubAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	return nil, nil, nil
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (a *stubAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
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
			out: "sum by(label) ()",
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
			out: "min() > 1",
			queries: []string{
				"min(foo) > 1 @ 10000",
			},
		},
		{
			in:  "max(foo{}) > 1",
			out: "max() > 1",
			queries: []string{
				"max(foo) > 1 @ 10000",
			},
		},
		{
			in:  "topk(5, foo{}) > 1",
			out: "topk(5, ) > 1",
			queries: []string{
				"topk(5, foo) > 1 @ 10000",
			},
		},
		{
			in:  "bottomk(5, foo{}) > 1",
			out: "bottomk(5, ) > 1",
			queries: []string{
				"bottomk(5, foo) > 1 @ 10000",
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
