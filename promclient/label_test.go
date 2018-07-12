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
