package promclient

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	model "github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"
)

func TestMergeLabelValues(t *testing.T) {
	tests := []struct {
		a      []model.LabelValue
		b      []model.LabelValue
		merged []model.LabelValue
	}{
		{
			a:      []model.LabelValue{"a"},
			b:      []model.LabelValue{"b"},
			merged: []model.LabelValue{"a", "b"},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			merged := MergeLabelValues(test.a, test.b)

			if !reflect.DeepEqual(merged, test.merged) {
				t.Fatalf("doesn't match\nexpected=%v\nactual=%v", test.merged, merged)
			}
		})
	}
}

func TestMergeLabelSets(t *testing.T) {
	tests := []struct {
		a      []model.LabelSet
		b      []model.LabelSet
		merged []model.LabelSet
	}{
		// Basic merge
		{
			a: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
			b: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hostb")},
			},
			merged: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
				{model.MetricNameLabel: model.LabelValue("hostb")},
			},
		},
		// No merge
		{
			a: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
			b: []model.LabelSet{},
			merged: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
		},
		// Dupe
		{
			a: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
			b: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
			merged: []model.LabelSet{
				{model.MetricNameLabel: model.LabelValue("hosta")},
			},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			merged := MergeLabelSets(test.a, test.b)

			if !reflect.DeepEqual(merged, test.merged) {
				t.Fatalf("doesn't match\nexpected=%v\nactual=%v", test.merged, merged)
			}
		})
	}
}

type labelStubAPI struct {
	series []model.LabelSet
}

func (a *labelStubAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	series := append([]model.LabelSet{}, a.series...)
	for _, m := range matchers {
		selectors, err := parser.ParseMetricSelector(m)
		if err != nil {
			return nil, nil, err
		}
		for _, selector := range selectors {
			for i, s := range series {
				v, ok := s[model.LabelName(selector.Name)]
				if !ok || !selector.Matches(string(v)) {
					series = append(series[:i], series[i+1:]...)
				}
			}
		}
	}

	names := make(map[string]struct{})
	for _, s := range series {
		for k := range s {
			if strings.HasPrefix(string(k), model.ReservedLabelPrefix) {
				continue
			}
			names[string(k)] = struct{}{}
		}
	}
	ret := make([]string, 0, len(names))
	for k := range names {
		ret = append(ret, k)
	}
	return ret, nil, nil
}

func (a *labelStubAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	series := append([]model.LabelSet{}, a.series...)
	for _, m := range matchers {
		selectors, err := parser.ParseMetricSelector(m)
		if err != nil {
			return nil, nil, err
		}
		for _, selector := range selectors {
			for i, s := range series {
				v, ok := s[model.LabelName(selector.Name)]
				if !ok || !selector.Matches(string(v)) {
					series = append(series[:i], series[i+1:]...)
				}
			}
		}
	}
	values := make(map[string]struct{})
	for _, s := range series {
		for k, v := range s {
			if strings.HasPrefix(string(k), model.ReservedLabelPrefix) {
				continue
			}
			values[string(v)] = struct{}{}
		}
	}
	ret := make(model.LabelValues, 0, len(values))
	for k := range values {
		ret = append(ret, model.LabelValue(k))
	}
	return ret, nil, nil
}

// Query performs a query for the given time.
func (a *labelStubAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")

}

// QueryRange performs a query for the given range.
func (a *labelStubAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")
}

// Series finds series by label matchers.
func (a *labelStubAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")

}

// GetValue loads the raw data for a given set of matchers in the time range
func (a *labelStubAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("not implemented")

}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (a *labelStubAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return nil, fmt.Errorf("not implemented")
}
func TestAddLabelClient(t *testing.T) {

	stub := &labelStubAPI{
		series: []model.LabelSet{{model.MetricNameLabel: "testmetric", "a": "1"}},
	}

	tests := []struct {
		labelSet    model.LabelSet
		err         bool
		matchers    []string
		labelValues []string
		labelNames  []string
	}{
		{
			labelSet:    model.LabelSet{"b": "1"},
			labelValues: []string{"1"},
			labelNames:  []string{"a", "b"},
		},
		{
			labelSet:    model.LabelSet{"b": "1"},
			labelValues: []string{"1"},
			labelNames:  []string{"a", "b"},
			matchers:    []string{`{b="1"}`},
		},
		{
			labelSet:    model.LabelSet{"b": "1", "c": "1"},
			labelValues: []string{"1"},
			labelNames:  []string{"a", "b", "c"},
			matchers:    []string{`{b="1", c="1"}`},
		},
		{
			labelSet:    model.LabelSet{"b": "1", "c": "1", "d": "1"},
			labelValues: []string{"1"},
			labelNames:  []string{"a", "b", "c", "d"},
			matchers:    []string{`{b="1", c="1"}`},
		},
		{
			labelSet:    model.LabelSet{"b": "1", "c": "1", "d": "1"},
			labelValues: []string{"1"},
			labelNames:  []string{"a", "b", "c", "d"},
			matchers:    []string{`{a="1", b="1", c="1"}`},
		},
	}

	for i, test := range tests {
		a := &AddLabelClient{stub, test.labelSet}
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			t.Run("LabelNames", func(t *testing.T) {
				v, _, err := a.LabelValues(context.TODO(), "a", test.matchers, time.Time{}, time.Time{})
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
						if actualV != model.LabelValue(test.labelValues[i]) {
							t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", test.labelValues, v)
						}
					}
				}
			})

			t.Run("LabelValues", func(t *testing.T) {
				v, _, err := a.LabelNames(context.TODO(), test.matchers, time.Time{}, time.Time{})
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

					for _, actualV := range v {
						if !slices.Contains(test.labelNames, actualV) {
							t.Fatalf("mismatch in value: \nexpected=%v\nactual=%v", test.labelNames, v)
						}
					}
				}
			})

		})
	}
}
