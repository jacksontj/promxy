package promclient

import (
	"context"
	"fmt"
	"strconv"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
)

type stubAPI struct {
	labelNames  func() []string
	labelValues func() model.LabelValues
	query       func() model.Value
	queryRange  func() model.Value
	series      func() []model.LabelSet
	getValue    func() model.Value
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *stubAPI) LabelNames(ctx context.Context) ([]string, v1.Warnings, error) {
	return s.labelNames(), nil, nil
}

// LabelValues performs a query for the values of the given label.
func (s *stubAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, v1.Warnings, error) {
	return s.labelValues(), nil, nil
}

// Query performs a query for the given time.
func (s *stubAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	return s.query(), nil, nil
}

// QueryRange performs a query for the given range.
func (s *stubAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	return s.queryRange(), nil, nil
}

// Series finds series by label matchers.
func (s *stubAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return s.series(), nil, nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *stubAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	return s.getValue(), nil, nil
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

func (s *errorAPI) LabelNames(ctx context.Context) ([]string, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.LabelNames(ctx)
}

// LabelValues performs a query for the values of the given label.
func (s *errorAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.LabelValues(ctx, label)
}

// Query performs a query for the given time.
func (s *errorAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *errorAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
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
func (s *errorAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	if s.err != nil {
		return nil, nil, s.err
	}
	return s.GetValue(ctx, start, end, matchers)
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

		labelValues: func() model.LabelValues {
			return model.LabelValues{}
		},
		query: func() model.Value {
			return model.Vector{
				getSample(model.LabelSet{model.MetricNameLabel: "testmetric"}),
			}
		},
		queryRange: func() model.Value {
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
			a: NewMultiAPI([]API{
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), nil, 1),
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
			a: NewMultiAPI([]API{
				NewMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				}, model.Time(0), nil, 1),
				NewMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), nil, 1),
			}, model.Time(0), nil, 2),
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
			a: NewMultiAPI([]API{
				NewMultiAPI([]API{
					NewMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"a": "1"}},
						&AddLabelClient{stub, model.LabelSet{"a": "1"}},
					}, model.Time(0), nil, 1),
					NewMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"a": "2"}},
						&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					}, model.Time(0), nil, 1),
				}, model.Time(0), nil, 2),
				NewMultiAPI([]API{
					NewMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"b": "1"}},
						&AddLabelClient{stub, model.LabelSet{"b": "1"}},
					}, model.Time(0), nil, 1),
					NewMultiAPI([]API{
						&AddLabelClient{stub, model.LabelSet{"b": "2"}},
						&AddLabelClient{stub, model.LabelSet{"b": "2"}},
					}, model.Time(0), nil, 1),
				}, model.Time(0), nil, 2),
			}, model.Time(0), nil, 2),
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
			a: NewMultiAPI([]API{
				NewMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
					&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				}, model.Time(0), nil, 1),
				NewMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "2"}}, fmt.Errorf("")},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), nil, 1),
			}, model.Time(0), nil, 2),
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
			a: NewMultiAPI([]API{
				NewMultiAPI([]API{
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
					&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				}, model.Time(0), nil, 1),
				NewMultiAPI([]API{
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
					&AddLabelClient{stub, model.LabelSet{"a": "2"}},
				}, model.Time(0), nil, 1),
			}, model.Time(0), nil, 2),
			err: true,
		},
		// if in a multi, all that "match" error, we should error
		{
			a: NewMultiAPI([]API{
				&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), nil, 1),
			err: true,
		},
		// however, in a multi if a single one succeeds for a given "group" then it should pass
		{
			a: NewMultiAPI([]API{
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&errorAPI{&AddLabelClient{stub, model.LabelSet{"a": "1"}}, fmt.Errorf("")},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), nil, 1),
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
			a: NewMultiAPI([]API{
				stub,
				&AddLabelClient{stub, model.LabelSet{"a": "1"}},
				&AddLabelClient{stub, model.LabelSet{"a": "2"}},
			}, model.Time(0), nil, 1),
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
				v, _, err := test.a.LabelNames(context.TODO())
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
				v, _, err := test.a.LabelValues(context.TODO(), "a")
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
				v, _, err := test.a.Query(context.TODO(), "testmetric", time.Now())
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if v.String() != test.v.String() {
						t.Fatalf("mismatch in value: \nexpected=%s\nactual=%s", test.v.String(), v.String())
					}
				} else {
					if test.v != nil {
						panic("tests that expect errors shouldn't have value set")
					}
				}
			})

			t.Run("QueryRange", func(t *testing.T) {
				v, _, err := test.a.QueryRange(context.TODO(), "testmetric", v1.Range{})
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if v.String() != test.v.String() {
						t.Fatalf("mismatch in value: \nexpected=%s\nactual=%s", test.v.String(), v.String())
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
				v, _, err := test.a.GetValue(context.TODO(), time.Now(), time.Now(), []*labels.Matcher{{
					Type:  labels.MatchEqual,
					Name:  "__name__",
					Value: "testmetric",
				}})
				if err != nil != test.err {
					if test.err {
						t.Fatalf("missing expected err")
					} else {
						t.Fatalf("Unexpected Err: %v", err)
					}
				}
				if err == nil {
					if v.String() != test.v.String() {
						t.Fatalf("mismatch in value: \nexpected=%s\nactual=%s", test.v.String(), v.String())
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
