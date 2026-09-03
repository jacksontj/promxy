package promclient

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

type stubAPI struct {
	labelNames     func() []string
	labelValues    func(label string) model.LabelValues
	query          func() model.Value
	queryRange     func(q string, r v1.Range) model.Value
	series         func() []model.LabelSet
	getValue       func() model.Value
	metadata       func() map[string][]v1.Metadata
	queryExemplars func() []v1.ExemplarQueryResult
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *stubAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	if s.labelNames == nil {
		return nil, nil, nil
	}
	return s.labelNames(), nil, nil
}

// LabelValues performs a query for the values of the given label.
func (s *stubAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if s.labelValues == nil {
		return nil, nil, nil
	}
	return s.labelValues(label), nil, nil
}

// Query performs a query for the given time.
func (s *stubAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if s.query == nil {
		return storage.EmptySeriesSet()
	}
	return ModelValueToSeriesSet(s.query(), nil, nil)
}

// QueryRange performs a query for the given range.
func (s *stubAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	if s.queryRange == nil {
		return storage.EmptySeriesSet()
	}
	return ModelValueToSeriesSet(s.queryRange(query, r), nil, nil)
}

// Series finds series by label matchers.
func (s *stubAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if s.series == nil {
		return nil, nil, nil
	}
	return s.series(), nil, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *stubAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	if s.getValue == nil {
		return storage.EmptySeriesSet()
	}
	return ModelValueToSeriesSet(s.getValue(), nil, nil)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *stubAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	if s.metadata == nil {
		return nil, nil
	}
	return s.metadata(), nil
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (s *stubAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	if s.queryExemplars == nil {
		return nil, nil
	}
	return s.queryExemplars(), nil
}

type errorAPI struct {
	API
	err error
}

func (s *errorAPI) Key() model.LabelSet {
	if apiLabels, ok := s.API.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}

func (s *errorAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.LabelNames(ctx, matchers, startTime, endTime)
}

// LabelValues performs a query for the values of the given label.
func (s *errorAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.LabelValues(ctx, label, matchers, startTime, endTime)
}

// Query performs a query for the given time.
func (s *errorAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if s.err != nil {
		return storage.ErrSeriesSet(s.err)
	}
	return s.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *errorAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	if s.err != nil {
		return storage.ErrSeriesSet(s.err)
	}
	return s.QueryRange(ctx, query, r)
}

// Series finds series by label matchers.
func (s *errorAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.Series(ctx, matches, startTime, endTime)
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *errorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	if s.err != nil {
		return storage.ErrSeriesSet(s.err)
	}
	return s.GetValue(ctx, start, end, matchers)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (s *errorAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.QueryExemplars(ctx, query, startTime, endTime)
}

func TestMultiAPIMerging(t *testing.T) {
	getSample := func(ls model.LabelSet) *model.Sample {
		return &model.Sample{
			Metric:    model.Metric(ls),
			Value:     model.SampleValue(1),
			Timestamp: model.Time(100),
		}
	}

	stub := &stubAPI{
		labelNames: func() []string {
			return []string{"a"}
		},

		labelValues: func(_ string) model.LabelValues {
			return model.LabelValues{}
		},
		query: func() model.Value {
			return model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric"}),
			}
		},
		queryRange: func(_ string, _ v1.Range) model.Value {
			return model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric"}),
			}
		},
		series: func() []model.LabelSet {
			return []model.LabelSet{{model.MetricNameLabel: "testmetric"}}
		},
		getValue: func() model.Value {
			return model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric"}),
			}
		},
	}

	tests := []struct {
		a           API
		labelNames  []string
		labelValues model.LabelValues
		v           model.Value
		series      []model.LabelSet
		err         bool
	}{
		// simple passthrough
		{
			a:          stub,
			labelNames: []string{"a"},
			v:          stub.query(),
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric"},
			},
		},
		// Ensure that simple label addition works
		{
			a:           &AddLabelClient{stub, model.LabelSet{"a": "b"}},
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"b"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "b"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "b"},
			},
		},
		// Ensure a single layer of multi merges
		{
			a: NewMustMultiAPI([]API{
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), false, nil, 1, false),
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
			},
		},
		// Ensure that a tree of multis work
		{
			a: NewMustMultiAPI([]API{
				NewMustMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				}, model.Time(0), false, nil, 1, false),
				NewMustMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), false, nil, 1, false),
			}, model.Time(0), false, nil, 2, false),
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
			},
		},
		// Ensure that a multi-level tree of multis work
		{
			a: NewMustMultiAPI([]API{
				NewMustMultiAPI([]API{
					NewMustMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"a": "1"}},
						&AddLabelClient{stub, model.LabelSet{"a": "1"}},
					}, model.Time(0), false, nil, 1, false),
					NewMustMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"a": "2"}},
						&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					}, model.Time(0), false, nil, 1, false),
				}, model.Time(0), false, nil, 2, false),
				NewMustMultiAPI([]API{
					NewMustMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"b": "1"}},
						&AddLabelClient{stub, model.LabelSet{"b": "1"}},
					}, model.Time(0), false, nil, 1, false),
					NewMustMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"b": "2"}},
						&AddLabelClient{stub, model.LabelSet{"b": "2"}},
					}, model.Time(0), false, nil, 1, false),
				}, model.Time(0), false, nil, 2, false),
			}, model.Time(0), false, nil, 2, false),
			labelNames:  []string{"a", "b"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "b": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "b": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
				{model.MetricNameLabel: "testmetric", "b": "1"},
				{model.MetricNameLabel: "testmetric", "b": "2"},
			},
		},

		// Check error conditions
		// simple passthrough
		{
			a:   &errorAPI{stub, fmt.Errorf("")},
			err: true,
		},
		// In a tree, if a single node errors for each, we should still return no-error
		{
			a: NewMustMultiAPI([]API{
				NewMustMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				}, model.Time(0), false, nil, 1, false),
				NewMustMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "2"}}, fmt.Errorf("")},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), false, nil, 1, false),
			}, model.Time(0), false, nil, 2, false),
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
			},
		},
		// In a tree, if any tree has all errors, we expect an error
		{
			a: NewMustMultiAPI([]API{
				NewMustMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				}, model.Time(0), false, nil, 1, false),
				NewMustMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), false, nil, 1, false),
			}, model.Time(0), false, nil, 2, false),
			err: true,
		},
		// if in a multi, all that "match" error, we should error
		{
			a: NewMustMultiAPI([]API{
				&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), false, nil, 1, false),
			err: true,
		},
		// however, in a multi if a single one succeeds for a given "group" then it should pass
		{
			a: NewMustMultiAPI([]API{
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), false, nil, 1, false),
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
			},
		},
		// multi with no labels
		{
			a: NewMustMultiAPI([]API{
				stub,
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), false, nil, 1, false),
			labelNames:  []string{"a"},
			labelValues: []model.LabelValue{"1", "2"},
			v: model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "1"}),
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric", "a": "2"}),
			},
			series: []model.LabelSet{
				{model.MetricNameLabel: "testmetric"},
				{model.MetricNameLabel: "testmetric", "a": "1"},
				{model.MetricNameLabel: "testmetric", "a": "2"},
			},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Run("LabelNames", func(t *testing.T) {
				v, _, err := test.a.LabelNames(context.TODO(), nil, time.Time{}, time.Time{})
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if len(v) != len(test.labelNames) {
						t.Fatalf("mismatch in len: \nexpected=%v\nactual=%v", test.labelNames, v)
					}

					for i, actualV := range v {
						if actualV != test.labelNames[i] {
							t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", test.labelNames, v)
						}
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("LabelValues", func(t *testing.T) {
				v, _, err := test.a.LabelValues(context.TODO(), "a", nil, time.Time{}, time.Time{})
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if len(v) != len(test.labelValues) {
						t.Fatalf("mismatch in len: \nexpected=%v\nactual=%v", test.labelValues, v)
					}

					for i, actualV := range v {
						if actualV != test.labelValues[i] {
							t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", test.labelValues, v)
						}
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("Query", func(t *testing.T) {
				ss := test.a.Query(context.TODO(), "testmetric", time.Now())
				err := ss.Err()
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if got, want := ssDataStrings(ss), valueDataStrings(test.v); !slices.Equal(got, want) {
						t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", want, got)
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("QueryRange", func(t *testing.T) {
				ss := test.a.QueryRange(context.TODO(), "testmetric", v1.Range{})
				err := ss.Err()
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if got, want := ssDataStrings(ss), valueDataStrings(test.v); !slices.Equal(got, want) {
						t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", want, got)
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("Series", func(t *testing.T) {
				v, _, err := test.a.Series(context.TODO(), []string{"testmetric"}, time.Now(), time.Now())
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if len(v) != len(test.series) {
						t.Fatalf("mismatch in len: \nexpected=%v\nactual=%v", test.series, v)
					}

					for i, actualV := range v {
						if !actualV.Equal(test.series[i]) {
							t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", test.series, v)
						}
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("GetValue", func(t *testing.T) {
				ss := test.a.GetValue(context.TODO(), time.Now(), time.Now(), []*labels.Matcher{{
					Type:  labels.MatchEqual,
					Name:  "__name__",
					Value: "testmetric",
				}})
				err := ss.Err()
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if got, want := ssDataStrings(ss), valueDataStrings(test.v); !slices.Equal(got, want) {
						t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", want, got)
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})
		})
	}
}

func ssDataStrings(ss storage.SeriesSet) []string {
	var out []string
	for ss.Next() {
		s := ss.At()
		var b strings.Builder
		b.WriteString(s.Labels().String())
		b.WriteString(" =>")
		it := s.Iterator(nil)
		for it.Next() != chunkenc.ValNone {
			t, v := it.At()
			fmt.Fprintf(&b, " %d=%g", t, v)
		}
		out = append(out, b.String())
	}
	sort.Strings(out)
	return out
}

func metricToLabelsT(m model.Metric) labels.Labels {
	b := labels.NewScratchBuilder(len(m))
	for k, v := range m {
		b.Add(string(k), string(v))
	}
	b.Sort()
	return b.Labels()
}

func valueDataStrings(v model.Value) []string {
	var out []string
	switch tv := v.(type) {
	case model.Vector:
		for _, s := range tv {
			out = append(out, fmt.Sprintf("%s => %d=%g", metricToLabelsT(s.Metric).String(), int64(s.Timestamp), float64(s.Value)))
		}
	case model.Matrix:
		for _, ss := range tv {
			var b strings.Builder
			b.WriteString(metricToLabelsT(ss.Metric).String())
			b.WriteString(" =>")
			for _, p := range ss.Values {
				fmt.Fprintf(&b, " %d=%g", int64(p.Timestamp), float64(p.Value))
			}
			out = append(out, b.String())
		}
	}
	sort.Strings(out)
	return out
}

func TestMultiAPIQueryExemplarsMerging(t *testing.T) {
	mkResult := func(series model.LabelSet, traceID string, val float64, ts int64) v1.ExemplarQueryResult {
		return v1.ExemplarQueryResult{
			SeriesLabels: series,
			Exemplars: []v1.Exemplar{
				{Labels: model.LabelSet{"trace_id": model.LabelValue(traceID)}, Value: model.SampleValue(val), Timestamp: model.Time(ts)},
			},
		}
	}

	tests := []struct {
		name          string
		api           API
		wantSeries    int // unique series in merged result
		wantExemplars int // total exemplars across all series
		wantErr       bool
	}{
		{
			// Two server-groups, both returning the SAME series label set
			// — exemplar lists must concatenate onto a single output entry.
			name: "merge same series across groups",
			api: NewMustMultiAPI([]API{
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "foo"}, "a", 1, 100)}
				}},
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "foo"}, "b", 2, 200)}
				}},
			}, model.Time(0), false, nil, 1, false),
			wantSeries:    1,
			wantExemplars: 2,
		},
		{
			// Different series in each group — both must appear.
			name: "different series across groups",
			api: NewMustMultiAPI([]API{
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "foo"}, "a", 1, 100)}
				}},
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "bar"}, "b", 2, 200)}
				}},
			}, model.Time(0), false, nil, 1, false),
			wantSeries:    2,
			wantExemplars: 2,
		},
		{
			// requiredCount=1, one error / one success → quorum met → success.
			name: "tolerates one error when requiredCount=1",
			api: NewMustMultiAPI([]API{
				&errorAPI{API: &stubAPI{}, err: fmt.Errorf("boom")},
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "foo"}, "a", 1, 100)}
				}},
			}, model.Time(0), false, nil, 1, false),
			wantSeries:    1,
			wantExemplars: 1,
		},
		{
			// requiredCount=2, only one stub is healthy → quorum fails.
			name: "errors when quorum unmet",
			api: NewMustMultiAPI([]API{
				&errorAPI{API: &stubAPI{}, err: fmt.Errorf("boom")},
				&stubAPI{queryExemplars: func() []v1.ExemplarQueryResult {
					return []v1.ExemplarQueryResult{mkResult(model.LabelSet{"__name__": "foo"}, "a", 1, 100)}
				}},
			}, model.Time(0), false, nil, 2, false),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.api.QueryExemplars(context.Background(), `foo`, time.Unix(0, 0), time.Unix(1, 0))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %d series", len(got))
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != tc.wantSeries {
				t.Fatalf("series count: want=%d got=%d (%+v)", tc.wantSeries, len(got), got)
			}
			total := 0
			for _, qr := range got {
				total += len(qr.Exemplars)
			}
			if total != tc.wantExemplars {
				t.Fatalf("exemplar count: want=%d got=%d (%+v)", tc.wantExemplars, total, got)
			}
		})
	}
}

func TestMultiAPIQueryExemplarsDedup(t *testing.T) {
	ex := func(traceID string, val float64, ts int64) v1.Exemplar {
		e := v1.Exemplar{Value: model.SampleValue(val), Timestamp: model.Time(ts)}
		if traceID != "" {
			e.Labels = model.LabelSet{"trace_id": model.LabelValue(traceID)}
		}
		return e
	}
	result := func(series model.LabelSet, exemplars ...v1.Exemplar) v1.ExemplarQueryResult {
		return v1.ExemplarQueryResult{SeriesLabels: series, Exemplars: exemplars}
	}
	stub := func(results ...v1.ExemplarQueryResult) API {
		return &stubAPI{queryExemplars: func() []v1.ExemplarQueryResult { return results }}
	}
	foo := model.LabelSet{"__name__": "foo"}
	bar := model.LabelSet{"__name__": "bar"}

	tests := []struct {
		name string
		apis []API
		want []v1.ExemplarQueryResult
	}{
		{
			// HA replicas of the same prometheus observed the same exemplar;
			// it must be returned exactly once.
			name: "identical exemplar from two replicas is deduplicated",
			apis: []API{
				stub(result(foo, ex("a", 1, 100))),
				stub(result(foo, ex("a", 1, 100))),
			},
			want: []v1.ExemplarQueryResult{result(foo, ex("a", 1, 100))},
		},
		{
			// Same timestamp, different trace IDs -- legitimately distinct
			// exemplars, both must be kept.
			name: "same timestamp with different labels are both kept",
			apis: []API{
				stub(result(foo, ex("a", 1, 100))),
				stub(result(foo, ex("b", 1, 100))),
			},
			want: []v1.ExemplarQueryResult{result(foo, ex("a", 1, 100), ex("b", 1, 100))},
		},
		{
			// Same timestamp and labels but a different value is a distinct exemplar.
			name: "same timestamp and labels with different values are both kept",
			apis: []API{
				stub(result(foo, ex("a", 1, 100))),
				stub(result(foo, ex("a", 2, 100))),
			},
			want: []v1.ExemplarQueryResult{result(foo, ex("a", 1, 100), ex("a", 2, 100))},
		},
		{
			name: "exemplars are sorted by timestamp",
			apis: []API{
				stub(result(foo, ex("c", 3, 300), ex("a", 1, 100))),
				stub(result(foo, ex("d", 4, 400), ex("b", 2, 200))),
			},
			want: []v1.ExemplarQueryResult{
				result(foo, ex("a", 1, 100), ex("b", 2, 200), ex("c", 3, 300), ex("d", 4, 400)),
			},
		},
		{
			name: "different series stay separate and are sorted by series labels",
			apis: []API{
				stub(result(foo, ex("a", 1, 100)), result(bar, ex("b", 2, 200))),
				stub(result(foo, ex("a", 1, 100)), result(bar, ex("b", 2, 200))),
			},
			want: []v1.ExemplarQueryResult{
				result(bar, ex("b", 2, 200)),
				result(foo, ex("a", 1, 100)),
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			api := NewMustMultiAPI(tc.apis, model.Time(0), false, nil, 1, false)
			got, err := api.QueryExemplars(context.Background(), `foo`, time.Unix(0, 0), time.Unix(1, 0))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("result mismatch\nwant: %+v\ngot:  %+v", tc.want, got)
			}
		})
	}
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *errorAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.API.Metadata(ctx, metric, limit)
}

// blockingAPI blocks every call until release is closed (or the context is
// cancelled). It's used to prove that the MultiAPI fan-out consumes results in
// completion order rather than in index order.
type blockingAPI struct {
	API
	release <-chan struct{}
}

func (s *blockingAPI) Key() model.LabelSet {
	if apiLabels, ok := s.API.(APILabels); ok {
		return apiLabels.Key()
	}
	return nil
}

func (s *blockingAPI) wait(ctx context.Context) error {
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *blockingAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	if err := s.wait(ctx); err != nil {
		return nil, nil, err
	}
	return s.API.LabelNames(ctx, matchers, startTime, endTime)
}

func (s *blockingAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	if err := s.wait(ctx); err != nil {
		return nil, nil, err
	}
	return s.API.LabelValues(ctx, label, matchers, startTime, endTime)
}

func (s *blockingAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	if err := s.wait(ctx); err != nil {
		return storage.ErrSeriesSet(err)
	}
	return s.API.Query(ctx, query, ts)
}

func (s *blockingAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	if err := s.wait(ctx); err != nil {
		return storage.ErrSeriesSet(err)
	}
	return s.API.QueryRange(ctx, query, r)
}

func (s *blockingAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	if err := s.wait(ctx); err != nil {
		return nil, nil, err
	}
	return s.API.Series(ctx, matches, startTime, endTime)
}

func (s *blockingAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	if err := s.wait(ctx); err != nil {
		return storage.ErrSeriesSet(err)
	}
	return s.API.GetValue(ctx, start, end, matchers)
}

func (s *blockingAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return s.API.Metadata(ctx, metric, limit)
}

func (s *blockingAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	if err := s.wait(ctx); err != nil {
		return nil, err
	}
	return s.API.QueryExemplars(ctx, query, startTime, endTime)
}

// TestMultiAPIEarlyReturnOnLaterIndexFailure asserts that a fatal failure from
// a later-indexed API aborts the fan-out immediately, instead of waiting for a
// slow earlier-indexed API to finish first. Index 0 blocks for the whole
// duration of the call (it is only released on the way out of the subtest) and
// index 1 fails right away; with requiredCount=2 that single failure makes the
// result impossible to assemble, so every method must return promptly.
func TestMultiAPIEarlyReturnOnLaterIndexFailure(t *testing.T) {
	const timeout = 10 * time.Second

	run := func(t *testing.T, call func(api *MultiAPI) error) {
		t.Helper()

		release := make(chan struct{})
		// The blocked index-0 goroutine is only ever released here, so it
		// cannot leak past the subtest -- and the call under test must have
		// returned before this runs.
		defer close(release)

		// Neither API has a Key(), so both land in a single fingerprint
		// bucket; requiredCount=2 makes one error fatal for that bucket.
		api := NewMustMultiAPI([]API{
			&blockingAPI{API: &stubAPI{}, release: release},
			&errorAPI{API: &stubAPI{}, err: fmt.Errorf("boom")},
		}, model.Time(0), false, nil, 2, false)

		done := make(chan error, 1)
		go func() { done <- call(api) }()

		select {
		case err := <-done:
			if err == nil {
				t.Fatalf("expected an error from the failing index-1 API, got nil")
			}
		case <-time.After(timeout):
			t.Fatalf("call did not return within %s: it is waiting on the slow index-0 API instead of consuming results as they arrive", timeout)
		}
	}

	t.Run("LabelValues", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			_, _, err := api.LabelValues(context.Background(), "a", nil, time.Time{}, time.Time{})
			return err
		})
	})

	t.Run("LabelNames", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			_, _, err := api.LabelNames(context.Background(), nil, time.Time{}, time.Time{})
			return err
		})
	})

	t.Run("Query", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			return api.Query(context.Background(), "testmetric", time.Now()).Err()
		})
	})

	t.Run("QueryRange", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			return api.QueryRange(context.Background(), "testmetric", v1.Range{}).Err()
		})
	})

	t.Run("GetValue", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			return api.GetValue(context.Background(), time.Now(), time.Now(), nil).Err()
		})
	})

	t.Run("Series", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			_, _, err := api.Series(context.Background(), []string{"testmetric"}, time.Now(), time.Now())
			return err
		})
	})

	t.Run("Metadata", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			_, err := api.Metadata(context.Background(), "testmetric", "")
			return err
		})
	})

	t.Run("QueryExemplars", func(t *testing.T) {
		run(t, func(api *MultiAPI) error {
			_, err := api.QueryExemplars(context.Background(), "testmetric", time.Unix(0, 0), time.Unix(1, 0))
			return err
		})
	})
}

// TestMultiAPIMergeIndependentOfCompletionOrder asserts that consuming results
// out of index order doesn't change the merged output: index 0 is held back so
// index 1's result lands first, and the merged result must match the merge of
// the same two APIs completing in index order.
func TestMultiAPIMergeIndependentOfCompletionOrder(t *testing.T) {
	const timeout = 10 * time.Second

	getSample := func(ls model.LabelSet) *model.Sample {
		return &model.Sample{
			Metric:    model.Metric(ls),
			Value:     model.SampleValue(1),
			Timestamp: model.Time(100),
		}
	}
	mkStub := func(v model.LabelValue) API {
		return &AddLabelClient{&stubAPI{
			labelNames:  func() []string { return []string{"a"} },
			labelValues: func(_ string) model.LabelValues { return model.LabelValues{} },
			query: func() model.Value {
				return model.Vector{getSample(model.LabelSet{model.MetricNameLabel: "testmetric"})}
			},
			queryRange: func(_ string, _ v1.Range) model.Value {
				return model.Vector{getSample(model.LabelSet{model.MetricNameLabel: "testmetric"})}
			},
			series: func() []model.LabelSet {
				return []model.LabelSet{{model.MetricNameLabel: "testmetric"}}
			},
			getValue: func() model.Value {
				return model.Vector{getSample(model.LabelSet{model.MetricNameLabel: "testmetric"})}
			},
		}, model.LabelSet{"a": v}}
	}

	run := func(t *testing.T, call func(api *MultiAPI) any) {
		t.Helper()

		// Baseline: both APIs complete in index order.
		want := call(NewMustMultiAPI([]API{mkStub("1"), mkStub("2")}, model.Time(0), false, nil, 1, false))

		release := make(chan struct{})
		var once sync.Once
		releaseFn := func() { once.Do(func() { close(release) }) }
		defer releaseFn()

		delayed := NewMustMultiAPI([]API{
			&blockingAPI{API: mkStub("1"), release: release},
			mkStub("2"),
		}, model.Time(0), false, nil, 1, false)

		done := make(chan any, 1)
		go func() { done <- call(delayed) }()

		// Let index 1's result land in the shared channel first, then let
		// index 0 proceed.
		time.Sleep(50 * time.Millisecond)
		releaseFn()

		select {
		case got := <-done:
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("out-of-order merge differs from in-order merge\nin-order    =%#v\nout-of-order=%#v", want, got)
			}
		case <-time.After(timeout):
			t.Fatalf("call did not return within %s", timeout)
		}
	}

	t.Run("LabelValues", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			v, _, err := api.LabelValues(context.Background(), "a", nil, time.Time{}, time.Time{})
			if err != nil {
				return err.Error()
			}
			return v
		})
	})

	t.Run("LabelNames", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			v, _, err := api.LabelNames(context.Background(), nil, time.Time{}, time.Time{})
			if err != nil {
				return err.Error()
			}
			return v
		})
	})

	t.Run("Query", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			return ssDataStrings(api.Query(context.Background(), "testmetric", time.Now()))
		})
	})

	t.Run("QueryRange", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			return ssDataStrings(api.QueryRange(context.Background(), "testmetric", v1.Range{}))
		})
	})

	t.Run("GetValue", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			return ssDataStrings(api.GetValue(context.Background(), time.Now(), time.Now(), []*labels.Matcher{{
				Type:  labels.MatchEqual,
				Name:  "__name__",
				Value: "testmetric",
			}}))
		})
	})

	t.Run("Series", func(t *testing.T) {
		run(t, func(api *MultiAPI) any {
			v, _, err := api.Series(context.Background(), []string{"testmetric"}, time.Now(), time.Now())
			if err != nil {
				return err.Error()
			}
			return v
		})
	})
}
