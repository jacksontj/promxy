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
		name         string
		a            model.Value
		b            model.Value
		r            model.Value
		antiAffinity model.Time
		err          error
	}{
		//
		// edge-cases
		{
			name: "nils",
			a:    nil,
			b:    nil,
			r:    nil,
		},

		{
			name: "bnil",
			a:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			b:    nil,
			r:    &model.Scalar{model.SampleValue(10), model.Time(100)},
		},

		{
			name: "anil",
			a:    nil,
			b:    &model.Scalar{model.SampleValue(10), model.Time(100)},
			r:    &model.Scalar{model.SampleValue(10), model.Time(100)},
		},

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
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
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
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hostb")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hostb")}),
					[]model.SamplePair{{
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
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(200),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(100),
							model.SampleValue(10),
						},
						{
							model.Time(200),
							model.SampleValue(10),
						},
					},
				},
			}),
		},

		// Fill missing
		// Lots of holes, ensure they are merged correctly
		{
			name: "Matrix fill missing2",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(200),
							model.SampleValue(10),
						},
						{
							model.Time(400),
							model.SampleValue(10),
						},
					},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(100),
							model.SampleValue(10),
						},
						{
							model.Time(300),
							model.SampleValue(10),
						},
						{
							model.Time(500),
							model.SampleValue(10),
						},
					},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(100),
							model.SampleValue(10),
						},
						{
							model.Time(200),
							model.SampleValue(10),
						},
						{
							model.Time(300),
							model.SampleValue(10),
						},
						{
							model.Time(400),
							model.SampleValue(10),
						},
						{
							model.Time(500),
							model.SampleValue(10),
						},
					},
				},
			}),
			antiAffinity: model.Time(20),
		},

		// Fill missing
		// In this case we have 2 series which have large gaps, but the anti-affinity
		// defines that we should not merge them, make sure we don't
		{
			name: "Matrix fill missing3",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(200),
							model.SampleValue(10),
						},
						{
							model.Time(400),
							model.SampleValue(10),
						},
					},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(100),
							model.SampleValue(10),
						},
						{
							model.Time(300),
							model.SampleValue(10),
						},
						{
							model.Time(500),
							model.SampleValue(10),
						},
					},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{
						{
							model.Time(200),
							model.SampleValue(10),
						},
						{
							model.Time(400),
							model.SampleValue(10),
						},
					},
				},
			}),
			antiAffinity: model.Time(100),
		},

		// Ensure that anti-affinity-buffer is working properly
		// if we have 2 matrix values with similar times only one should be put in
		{
			name: "Matrix merge similar",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(101),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			antiAffinity: model.Time(2),
		},

		// Ensure that anti-affinity-buffer is working properly
		// we want to prefer balues from the "first" series (as that is the one
		// we already have. This avoids unnecessary switches between series
		{
			name: "Matrix merge similar 2",
			a: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(101),
						model.SampleValue(10),
					}},
				},
			}),
			b: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(100),
						model.SampleValue(10),
					}},
				},
			}),
			r: model.Matrix([]*model.SampleStream{
				{
					model.Metric(model.LabelSet{model.MetricNameLabel: model.LabelValue("hosta")}),
					[]model.SamplePair{{
						model.Time(101),
						model.SampleValue(10),
					}},
				},
			}),
			antiAffinity: model.Time(2),
		},
	}

	for _, test := range tests {
		result, err := MergeValues(test.antiAffinity, test.a, test.b)
		if err != test.err {
			t.Fatalf("mismatch err in %s expected=%v actual=%v", test.name, test.err, err)
		}
		if !reflect.DeepEqual(result, test.r) {
			t.Fatalf("mismatch in %s \nexpected=%v\nactual=%v", test.name, test.r, result)
		}
	}

}
