package proxystorage

import (
	"context"
	"reflect"
	"sort"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
)

type exemplarStubAPI struct {
	stubAPI
	calls []exemplarCall
	resps map[string][]v1.ExemplarQueryResult
}

type exemplarCall struct {
	query string
	start time.Time
	end   time.Time
}

func (a *exemplarStubAPI) QueryExemplars(_ context.Context, query string, start, end time.Time) ([]v1.ExemplarQueryResult, error) {
	a.calls = append(a.calls, exemplarCall{query, start, end})
	return a.resps[query], nil
}

func TestProxyExemplarQuerier_Select(t *testing.T) {
	api := &exemplarStubAPI{
		resps: map[string][]v1.ExemplarQueryResult{
			`{__name__="foo",job="api"}`: {
				{
					SeriesLabels: model.LabelSet{"__name__": "foo", "job": "api"},
					Exemplars: []v1.Exemplar{
						{Labels: model.LabelSet{"trace_id": "abc"}, Value: 1.5, Timestamp: 1500},
					},
				},
			},
			`{__name__="bar"}`: {
				{
					SeriesLabels: model.LabelSet{"__name__": "bar"},
					Exemplars: []v1.Exemplar{
						{Labels: model.LabelSet{"trace_id": "xyz"}, Value: 0.25, Timestamp: 2000},
					},
				},
			},
		},
	}

	q := &proxyExemplarQuerier{ctx: context.Background(), client: api}

	fooMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "__name__", "foo"),
		labels.MustNewMatcher(labels.MatchEqual, "job", "api"),
	}
	barMatchers := []*labels.Matcher{
		labels.MustNewMatcher(labels.MatchEqual, "__name__", "bar"),
	}

	got, err := q.Select(1000, 3000, fooMatchers, barMatchers)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return got[i].SeriesLabels.Get("__name__") < got[j].SeriesLabels.Get("__name__") })

	want := []exemplar.QueryResult{
		{
			SeriesLabels: labels.FromStrings("__name__", "bar"),
			Exemplars: []exemplar.Exemplar{
				{Labels: labels.FromStrings("trace_id", "xyz"), Value: 0.25, Ts: 2000, HasTs: true},
			},
		},
		{
			SeriesLabels: labels.FromStrings("__name__", "foo", "job", "api"),
			Exemplars: []exemplar.Exemplar{
				{Labels: labels.FromStrings("trace_id", "abc"), Value: 1.5, Ts: 1500, HasTs: true},
			},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("result mismatch\nwant: %#v\ngot:  %#v", want, got)
	}

	// Each selector becomes one downstream query; time params come from Select.
	if len(api.calls) != 2 {
		t.Fatalf("want 2 downstream calls, got %d (%+v)", len(api.calls), api.calls)
	}
	for _, c := range api.calls {
		if c.start.UnixMilli() != 1000 || c.end.UnixMilli() != 3000 {
			t.Errorf("unexpected time params: %+v", c)
		}
	}
}

func TestProxyExemplarQuerier_DeduplicatesAcrossSelectors(t *testing.T) {
	// Same series labels returned for two different selectors — exemplars
	// from both calls should land on the single output series, not two
	// separate ones with duplicate metadata.
	dup := v1.ExemplarQueryResult{
		SeriesLabels: model.LabelSet{"__name__": "foo"},
	}
	api := &exemplarStubAPI{
		resps: map[string][]v1.ExemplarQueryResult{
			`{__name__="foo",job="a"}`: {
				func() v1.ExemplarQueryResult {
					r := dup
					r.Exemplars = []v1.Exemplar{{Value: 1, Timestamp: 100}}
					r.SeriesLabels = model.LabelSet{"__name__": "foo"}
					return r
				}(),
			},
			`{__name__="foo",job="b"}`: {
				func() v1.ExemplarQueryResult {
					r := dup
					r.Exemplars = []v1.Exemplar{{Value: 2, Timestamp: 200}}
					r.SeriesLabels = model.LabelSet{"__name__": "foo"}
					return r
				}(),
			},
		},
	}

	q := &proxyExemplarQuerier{ctx: context.Background(), client: api}
	got, err := q.Select(0, 1000,
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "__name__", "foo"),
			labels.MustNewMatcher(labels.MatchEqual, "job", "a"),
		},
		[]*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, "__name__", "foo"),
			labels.MustNewMatcher(labels.MatchEqual, "job", "b"),
		},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 merged series, got %d", len(got))
	}
	if len(got[0].Exemplars) != 2 {
		t.Fatalf("want 2 exemplars on the merged series, got %d", len(got[0].Exemplars))
	}
}
