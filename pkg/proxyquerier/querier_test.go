package proxyquerier

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promclient"
)

func mustSelector(t *testing.T, s string) []*labels.Matcher {
	t.Helper()
	m, err := parser.ParseMetricSelector(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return m
}

func drainSeries(t *testing.T, ss storage.SeriesSet) []labels.Labels {
	t.Helper()
	var out []labels.Labels
	for ss.Next() {
		out = append(out, ss.At().Labels())
	}
	if err := ss.Err(); err != nil {
		t.Fatalf("series set error: %v", err)
	}
	return out
}

func drainLabels(t *testing.T, ss storage.SeriesSet) []string {
	t.Helper()
	var out []string
	for _, l := range drainSeries(t, ss) {
		out = append(out, l.String())
	}
	return out
}

// newTestQuerier stands up a real downstream Prometheus API server over the
// given promql test data and wraps it in the same client stack a single-target
// server group builds: AddLabelClient (server-group labels) over a MultiAPI.
func newTestQuerier(t *testing.T, data string, sgLabels model.LabelSet) *ProxyQuerier {
	t.Helper()

	path := filepath.Join(t.TempDir(), "data.test")
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write test data: %v", err)
	}
	api, closeFn, err := promclient.CreateTestServer(t, path)
	if closeFn != nil {
		t.Cleanup(closeFn)
	}
	if err != nil {
		t.Fatalf("create test server: %v", err)
	}

	multi, err := promclient.NewMultiAPI(
		[]promclient.API{&promclient.AddLabelClient{API: api, Labels: sgLabels}},
		0, false, nil, 1, false,
	)
	if err != nil {
		t.Fatalf("new multi api: %v", err)
	}

	return &ProxyQuerier{
		Start:  timestamp.Time(selectStart).UTC(),
		End:    timestamp.Time(selectEnd).UTC(),
		Client: multi,
		Cfg:    &proxyconfig.PromxyConfig{},
	}
}

const (
	selectStart = int64(60000)
	selectEnd   = int64(120000)
)

// A single-target server group whose labels collide with a downstream label
// reorders the downstream's (correctly sorted) result: overwriting job="a"/
// job="b" with job="sg" turns `up{job="a",pod="x"} < up{job="b"}` into
// `up{job="sg",pod="x"} > up{job="sg"}`. Nothing below Select re-sorts in that
// configuration -- MergeSeriesSets passes a lone set straight through -- so
// Select is the only place the sortSeries contract can be honored.
const collidingLabelData = `load 1m
  up{job="a",pod="x"} 1 1 1
  up{job="b"} 2 2 2
`

// TestSelectHonorsSortSeries asserts Select(sortSeries=true) returns series in
// labels.Compare order even when the client stack has reordered the
// downstream's result, and that Select(sortSeries=false) passes the result
// through untouched.
func TestSelectHonorsSortSeries(t *testing.T) {
	q := newTestQuerier(t, collidingLabelData, model.LabelSet{"job": "sg"})
	hints := &storage.SelectHints{Start: selectStart, End: selectEnd}
	sel := mustSelector(t, `{__name__="up"}`)

	unsorted := drainLabels(t, q.Select(context.Background(), false, hints, sel...))
	want := []string{`{__name__="up", job="sg", pod="x"}`, `{__name__="up", job="sg"}`}
	if len(unsorted) != len(want) || unsorted[0] != want[0] || unsorted[1] != want[1] {
		t.Fatalf("sortSeries=false should pass the client's order through\n got: %v\nwant: %v", unsorted, want)
	}

	sorted := drainSeries(t, q.Select(context.Background(), true, hints, sel...))
	for i := 1; i < len(sorted); i++ {
		if labels.Compare(sorted[i-1], sorted[i]) >= 0 {
			t.Fatalf("sortSeries=true returned unsorted series: %v", sorted)
		}
	}
	if len(sorted) != len(unsorted) {
		t.Fatalf("sorting changed the series count: got %v want %v", sorted, unsorted)
	}
}

// TestSelectSortedFeedsMergeSeriesSet drives the pattern the /federate and
// /api/v1/series handlers use -- one Select per match[], all fed to
// storage.NewMergeSeriesSet -- and asserts no series is emitted twice. That
// k-way merge assumes each input is sorted and silently duplicates series when
// one is not.
func TestSelectSortedFeedsMergeSeriesSet(t *testing.T) {
	q := newTestQuerier(t, collidingLabelData, model.LabelSet{"job": "sg"})
	hints := &storage.SelectHints{Start: selectStart, End: selectEnd}

	// The second selector matches only the lexically-smaller of the two series,
	// so the merge's cursor advances past the first set's out-of-order head.
	sets := []storage.SeriesSet{
		q.Select(context.Background(), true, hints, mustSelector(t, `{__name__="up"}`)...),
		q.Select(context.Background(), true, hints, mustSelector(t, `{__name__="up",pod=""}`)...),
	}

	got := drainLabels(t, storage.NewMergeSeriesSet(sets, 0, storage.ChainedSeriesMerge))
	seen := make(map[string]int, len(got))
	for _, l := range got {
		seen[l]++
	}
	for l, n := range seen {
		if n > 1 {
			t.Fatalf("series %s emitted %d times by the merge: %v", l, n, got)
		}
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct series, got %v", got)
	}
}

// countingSeriesSet reports how many times it was advanced, so a test can tell
// whether a wrapper drained it.
type countingSeriesSet struct {
	series []storage.Series
	idx    int
	nexts  int
}

func (s *countingSeriesSet) Next() bool {
	s.nexts++
	if s.idx >= len(s.series) {
		return false
	}
	s.idx++
	return true
}
func (s *countingSeriesSet) At() storage.Series                { return s.series[s.idx-1] }
func (s *countingSeriesSet) Err() error                        { return nil }
func (s *countingSeriesSet) Warnings() annotations.Annotations { return nil }

// stubAPI serves a fixed SeriesSet from GetValue. The embedded nil API panics
// on any other call, which is what we want: nothing else should be reached.
type stubAPI struct {
	promclient.API
	ss        storage.SeriesSet
	labelsets []model.LabelSet
}

func (s *stubAPI) GetValue(_ context.Context, _, _ time.Time, _ []*labels.Matcher) storage.SeriesSet {
	return s.ss
}

func (s *stubAPI) Series(_ context.Context, _ []string, _, _ time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return s.labelsets, nil, nil
}

// TestSelectSeriesMetadataHonorsSortSeries covers the metadata branch of Select
// (hints.Func == "series"), which builds series from Client.Series. MultiAPI
// concatenates the per-target label sets, so the result arrives in target order.
func TestSelectSeriesMetadataHonorsSortSeries(t *testing.T) {
	q := &ProxyQuerier{
		Client: &stubAPI{labelsets: []model.LabelSet{
			{"__name__": "up", "pod": "x"},
			{"__name__": "up"},
		}},
		Cfg: &proxyconfig.PromxyConfig{},
	}
	hints := &storage.SelectHints{Func: "series"}
	sel := mustSelector(t, `{__name__="up"}`)

	got := drainLabels(t, q.Select(context.Background(), true, hints, sel...))
	want := []string{`{__name__="up"}`, `{__name__="up", pod="x"}`}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("sortSeries=true on the metadata path\n got: %v\nwant: %v", got, want)
	}

	if got := drainLabels(t, q.Select(context.Background(), false, hints, sel...)); got[0] != want[1] {
		t.Fatalf("sortSeries=false should pass the client's order through: %v", got)
	}
}

// TestSelectUnsortedDoesNotDrain pins the promql engine's hot path: it calls
// Select with sortSeries=false and the result must be handed back without being
// materialized, so a streaming client stays streaming.
func TestSelectUnsortedDoesNotDrain(t *testing.T) {
	src := &countingSeriesSet{series: []storage.Series{
		storage.NewListSeries(labels.FromStrings("__name__", "up", "pod", "x"), nil),
		storage.NewListSeries(labels.FromStrings("__name__", "up"), nil),
	}}
	q := &ProxyQuerier{Client: &stubAPI{ss: src}, Cfg: &proxyconfig.PromxyConfig{}}

	ss := q.Select(context.Background(), false, &storage.SelectHints{Start: selectStart, End: selectEnd})
	if src.nexts != 0 {
		t.Fatalf("Select(sortSeries=false) advanced the source %d times; it must not materialize", src.nexts)
	}
	if got := drainLabels(t, ss); len(got) != 2 || got[0] != `{__name__="up", pod="x"}` {
		t.Fatalf("Select(sortSeries=false) altered the result: %v", got)
	}
}
