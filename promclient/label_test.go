package promclient

import (
	"reflect"
	"strconv"
	"testing"

	model "github.com/prometheus/common/model"
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
