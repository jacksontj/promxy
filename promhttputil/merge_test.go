package promhttputil

import (
	"reflect"
	"testing"

	"github.com/prometheus/common/model"
)

/*
   ValNone ValueType = iota
   ValScalar
   ValVector
   ValMatrix
   ValString

*/
// Merge 2 values and
func TestMergeValues(t *testing.T) {
	tests := []struct {
		name string
		a    model.Value
		b    model.Value
		r    model.Value
		err  error
	}{
		//
		// Scalar tests
		{
			name: "scalar dedupe",
			a:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			b:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			r:    &model.Scalar{model.SampleValue(10), model.Time(100)},
		},

		// Fill missing
		{
			name: "scalar fill missing",
			a:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			b:    &model.Scalar{},
			r:    &model.Scalar{model.SampleValue(10), model.Time(100)},
		},

		// Fill missing
		{
			name: "scalar fill missing",
			a:    &model.Scalar{},
			b:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			r:    &model.Scalar{model.SampleValue(10), model.Time(100)},
		},

		//
		// String tests
		{
			name: "String dedupe",
			a:    &model.String{"a", model.Time(100)},
			b:    &model.String{"a", model.Time(100)},
			r:    &model.String{"a", model.Time(100)},
		},

		// Fill missing
		{
			name: "string fill missing",
			a:    &model.String{"a", model.Time(100)},
			b:    &model.String{"", model.Time(100)},
			r:    &model.String{"a", model.Time(100)},
		},

		// Fill missing
		{
			name: "string fill missing",
			a:    &model.String{"", model.Time(100)},
			b:    &model.String{"a", model.Time(100)},
			r:    &model.String{"a", model.Time(100)},
		},

		//
		// Vector tests
		{
			name: "Vector dedupe",
			a: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			b: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			r: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
		},

		{
			name: "Vector dedupe multiseries",
			a: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("metrica")}),
					model.SampleValue(10),
					model.Time(100),
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("metricb")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			b: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("metrica")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			r: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("metrica")}),
					model.SampleValue(10),
					model.Time(100),
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("metricb")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
		},

		// Fill missing
		{
			name: "Vector fill missing1",
			a: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			b: model.Vector([]*model.Sample{}),
			r: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
		},

		// Fill missing
		{
			name: "Vector fill missing2",
			a:    model.Vector([]*model.Sample{}),
			b: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
			r: model.Vector([]*model.Sample{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					model.SampleValue(10),
					model.Time(100),
				},
			}),
		},

		//
		// Matrix tests
		{
			name: "Matrix dedupe",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
		},

		{
			name: "Matrix dedupe multiseries",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hostb")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hostb")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
		},

		// Fill missing
		{
			name: "Matrix fill missing",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(200),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{model.SamplePair{
						model.Time(100),
						model.SampleValue(10),
					},
						model.SamplePair{
							model.Time(200),
							model.SampleValue(10),
						}},
				},
			}),
		},

		/*
			// Fill missing
			{
				name: "Matrix fill missing2",
				a:    model.Vector([]*model.Sample{}),
				b: model.Vector([]*model.Sample{
					{
						model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
						model.SampleValue(10),
						model.Time(100),
					},
				}),
				r: model.Vector([]*model.Sample{
					{
						model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
						model.SampleValue(10),
						model.Time(100),
					},
				}),
			}
		*/
	}

	for _, test := range tests {
		result, err := MergeValues(model.Time(0), test.a, test.b)
		if err != test.err {
			t.Fatalf("mismatch err in %s expected=%v actual=%v", test.name, test.err, err)
		}
		if !reflect.DeepEqual(result, test.r) {
			t.Fatalf("mismatch in %s expected=\n%v actual=\n%v", test.name, test.r, result)
		}
	}

}
