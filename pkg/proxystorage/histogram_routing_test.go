package proxystorage

import (
	"context"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/promql/parser"

	"github.com/jacksontj/promxy/pkg/servergroup"
)

func TestIsHistogramExpr(t *testing.T) {
	cases := []struct {
		name       string
		query      string
		histograms map[string]struct{} // metric names the metadata cache reports as histograms
		want       bool
	}{
		// Histogram-only function family — unambiguous, no cache needed.
		{name: "histogram_count", query: `histogram_count(foo)`, want: true},
		{name: "histogram_sum", query: `histogram_sum(foo)`, want: true},
		{name: "histogram_avg", query: `histogram_avg(foo)`, want: true},
		{name: "histogram_stddev", query: `histogram_stddev(foo)`, want: true},
		{name: "histogram_stdvar", query: `histogram_stdvar(foo)`, want: true},
		{name: "histogram_fraction", query: `histogram_fraction(0, 1, foo)`, want: true},

		// histogram_quantile is intentionally dual-mode (classic + native).
		// Without the metadata cache we must NOT flag it, or every classic
		// histogram query would be diverted to remote_read.
		{name: "histogram_quantile alone", query: `histogram_quantile(0.95, foo)`, want: false},

		// Nested under an aggregation — should still flag.
		{name: "sum over histogram_count", query: `sum(histogram_count(foo))`, want: true},
		// Nested under a binary expr — should still flag.
		{name: "histogram_sum / count", query: `histogram_sum(foo) / count(bar)`, want: true},
		// Function in RHS of binary expression.
		{name: "binary with histogram on RHS", query: `count(foo) + histogram_count(bar)`, want: true},

		// Plain float queries — no signal.
		{name: "bare selector", query: `up`, want: false},
		{name: "rate of float", query: `rate(foo[5m])`, want: false},
		{name: "sum", query: `sum(rate(foo[5m]))`, want: false},

		// Metadata cache leg — pure VectorSelector reference, no function.
		{
			name:       "vector selector hits cache",
			query:      `my_native_hist`,
			histograms: map[string]struct{}{"my_native_hist": {}},
			want:       true,
		},
		{
			name:       "rate over cached histogram",
			query:      `rate(my_native_hist[5m])`,
			histograms: map[string]struct{}{"my_native_hist": {}},
			want:       true,
		},
		{
			name:       "cache has different metric — no signal",
			query:      `rate(foo[5m])`,
			histograms: map[string]struct{}{"unrelated_hist": {}},
			want:       false,
		},
		{
			name:       "nil cache + plain selector",
			query:      `foo`,
			histograms: nil,
			want:       false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr, err := parser.ParseExpr(tc.query)
			if err != nil {
				t.Fatalf("parsing %q: %v", tc.query, err)
			}
			var pred func(string) bool
			if tc.histograms != nil {
				pred = func(name string) bool {
					_, ok := tc.histograms[name]
					return ok
				}
			}
			got := isHistogramExpr(context.Background(), expr, nil, pred)
			if got != tc.want {
				t.Fatalf("isHistogramExpr(%q) = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}

func TestStrictMissingRemoteRead(t *testing.T) {
	cases := []struct {
		name string
		sgs  []*servergroup.ServerGroup
		want []int
	}{
		{
			name: "all remote_read configured",
			sgs: []*servergroup.ServerGroup{
				{Cfg: &servergroup.Config{Ordinal: 0, RemoteRead: true}},
				{Cfg: &servergroup.Config{Ordinal: 1, RemoteRead: true}},
			},
			want: nil,
		},
		{
			name: "remote_read missing, allow_lossy off — strict",
			sgs: []*servergroup.ServerGroup{
				{Cfg: &servergroup.Config{Ordinal: 0, RemoteRead: false}},
			},
			want: []int{0},
		},
		{
			name: "remote_read missing, allow_lossy on — not strict",
			sgs: []*servergroup.ServerGroup{
				{Cfg: &servergroup.Config{
					Ordinal:         0,
					RemoteRead:      false,
					NativeHistogram: servergroup.NativeHistogramConfig{AllowLossy: true},
				}},
			},
			want: nil,
		},
		{
			name: "mixed — only the strict one shows up",
			sgs: []*servergroup.ServerGroup{
				{Cfg: &servergroup.Config{Ordinal: 0, RemoteRead: true}},
				{Cfg: &servergroup.Config{Ordinal: 1, RemoteRead: false}}, // strict
				{Cfg: &servergroup.Config{
					Ordinal:         2,
					RemoteRead:      false,
					NativeHistogram: servergroup.NativeHistogramConfig{AllowLossy: true},
				}},
			},
			want: []int{1},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ps := &ProxyStorage{}
			ps.state.Store(&proxyStorageState{sgs: tc.sgs})
			got := ps.strictMissingRemoteRead()
			if (len(got) == 0) != (len(tc.want) == 0) || !equalInts(got, tc.want) {
				t.Fatalf("strictMissingRemoteRead = %v, want %v", got, tc.want)
			}
		})
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestHistogramFidelityError(t *testing.T) {
	err := histogramFidelityError([]int{0, 2})
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"ordinal=[0 2]", "remote_read", "allow_lossy"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
}

// TestIsHistogramExpr_PathSignal verifies the path-based signal: when a
// histogram-only call is an ANCESTOR of the visited node (as happens when
// parser.Walk descends into the children of a node we've already skipped),
// the descendant is also flagged.
func TestIsHistogramExpr_PathSignal(t *testing.T) {
	// Build a path that mimics what parser.Walk passes when visiting the
	// inner VectorSelector of histogram_count(foo).
	outer, err := parser.ParseExpr(`histogram_count(foo)`)
	if err != nil {
		t.Fatal(err)
	}
	call := outer.(*parser.Call)
	inner := call.Args[0] // the VectorSelector "foo"

	// Without the path, the inner VectorSelector looks like a plain float
	// selector — no signal.
	if isHistogramExpr(context.Background(), inner, nil, nil) {
		t.Fatal("inner selector standalone should not flag")
	}

	// With the histogram_count Call in the path, it must flag.
	if !isHistogramExpr(context.Background(), inner, []parser.Node{call}, nil) {
		t.Fatal("inner selector under histogram_count should flag via path")
	}
}
