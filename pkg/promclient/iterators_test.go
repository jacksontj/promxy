package promclient

import (
	"strconv"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

func TestIterators(t *testing.T) {
	tests := []struct {
		val    interface{} // value
		labels labels.Labels
	}{
		{
			val: &model.Scalar{
				Value:     0,
				Timestamp: 0,
			},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			it := NewSeriesIterator(test.val)

			labels := it.Labels()
			if labels.String() != test.labels.String() {
				t.Fatalf("Mismatch in labels expected=%+v actual=%+v", test.labels, labels)
			}
		})
	}

}
