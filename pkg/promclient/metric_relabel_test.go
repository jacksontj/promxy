package promclient

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"k8s.io/utils/strings/slices"
)

func TestRewriteLabels(t *testing.T) {
	tests := []struct {
		cfgs []*MetricRelabelConfig
		in   []string
		out  []string
	}{
		// Test a simple Replace
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Replace,
				},
			},
			in:  []string{"dst"},
			out: []string{"src"},
		},

		// TODO: labeldrop
		// lowercase
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Lowercase,
				},
			},
			in:  []string{"dst"},
			out: []string{"src"},
		},
		// TODO: uppercase

	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			out := RewriteLabels(test.cfgs, test.in)
			if !reflect.DeepEqual(out, test.out) {
				t.Fatalf("Mismatch in labels after rewrite expected=%#v actual=%#v", test.out, out)
			}
		})
	}
}

func TestRewriteMatchers(t *testing.T) {
	tests := []struct {
		cfgs        []*MetricRelabelConfig
		matchersIn  []*labels.Matcher
		matchersOut []*labels.Matcher
		notOk       bool
	}{
		// Test a simple Replace
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: "src",
					TargetLabel: "dst",
					Action:      relabel.Replace,
				},
			},
			matchersIn:  []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "dst", "a")},
			matchersOut: []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "src", "a")},
		},

		// Test a simple LabelDrop
		// TODO
		// Test a simple Lowercase
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Lowercase,
				},
			},
			matchersIn:  []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "dst", "a")},
			matchersOut: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "src", "(?i)a")},
		},
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Lowercase,
				},
			},
			matchersIn:  []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "dst", "a")},
			matchersOut: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "src", "(?i)a")},
		},
		// Test a simple Uppercase
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Uppercase,
				},
			},
			matchersIn:  []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "dst", "a")},
			matchersOut: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "src", "(?i)a")},
		},
		{
			cfgs: []*MetricRelabelConfig{
				{
					SourceLabel: model.LabelName("src"),
					TargetLabel: "dst",
					Action:      relabel.Uppercase,
				},
			},
			matchersIn:  []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "dst", "a")},
			matchersOut: []*labels.Matcher{labels.MustNewMatcher(labels.MatchRegexp, "src", "(?i)a")},
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			out, ok := RewriteMatchers(test.cfgs, test.matchersIn)
			if ok == test.notOk {
				t.Fatalf("Mismatch in matchers after rewrite OK expected=%#v actual=%#v", !test.notOk, ok)
			}
			if !matchersEqual(out, test.matchersOut) {
				t.Fatalf("Mismatch in matchers after rewrite expected=%#v actual=%#v", test.matchersOut, out)
			}
		})
	}
}

func matchersEqual(a, b []*labels.Matcher) bool {
	if len(a) != len(b) {
		return false
	}

	for i, item := range a {
		if item.String() != b[i].String() {
			return false
		}
	}

	return true
}

// This is an "integration" test
func TestMetricRelabel(t *testing.T) {
	client, close, err := CreateTestServer(t, "testdata/metric_relabel.test")
	if err != nil {
		t.Fatal(err)
	}
	defer close()

	t.Run("labeldrop", func(t *testing.T) {
		const droplabel = "goversion" // Label to drop in this test case
		cfgs := []*MetricRelabelConfig{{SourceLabel: droplabel, Action: "labeldrop"}}
		c, err := NewMetricsRelabelClient(client, cfgs)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("LabelNames", func(t *testing.T) {
			// Basic LabelNames
			tests := [][]string{
				nil,
				{"prometheus_build_info"},
				{fmt.Sprintf(`{%s!=""}`, droplabel)},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelNames(context.TODO(), test, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					if slices.Contains(lbls, droplabel) {
						t.Fatalf("LabelNames contains sourcelabel label: %v", lbls)
					}
				})
			}
		})

		t.Run("LabelValues", func(t *testing.T) {
			tests := []struct {
				label    string
				matchers []string
			}{
				{
					label:    "job",
					matchers: nil,
				},
				{
					label:    "job",
					matchers: []string{`{job="prometheus"}`},
				},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelValues(context.TODO(), test.label, test.matchers, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}

					values := make([]string, len(lbls))
					for i, l := range lbls {
						values[i] = string(l)
					}

					if slices.Contains(values, "go1.16.4") {
						t.Fatalf("LabelValues contains dropped label")
					}
				})
			}
		})

		t.Run("Query", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info", // Test that we don't get a label back
				`{job="prometheus"}`,    // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, droplabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.Query(context.TODO(), query, model.Time(5).Time())
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Vector:
						for _, v := range []*model.Sample(valTyped) {
							if _, ok := v.Metric[droplabel]; ok {
								t.Fatalf("found droplabel in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("QueryRange", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info", // Test that we don't get a label back
				`{job="prometheus"}`,    // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, droplabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.QueryRange(context.TODO(), query, v1.Range{Start: model.Time(0).Time(), End: model.Time(10).Time(), Step: time.Duration(1e6)})
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[droplabel]; ok {
								t.Fatalf("found droplabel in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("Series", func(t *testing.T) {
			tests := [][]string{
				{`prometheus_build_info`},
				{`{job="prometheus"}`},
				{`{__name__=~".+"}`},
				{`{job="prometheus"}`, `{version="2.27.0"}`},
			}
			for i, matches := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lblsets, _, err := c.Series(context.TODO(), matches, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					for _, lblset := range lblsets {
						if _, ok := lblset[model.LabelName(droplabel)]; ok {
							t.Fatalf("found droplabel in response")
						}
					}
				})
			}
		})

		t.Run("GetValue", func(t *testing.T) {
			tests := [][]*labels.Matcher{
				{labels.MustNewMatcher(labels.MatchEqual, "__name__", "prometheus_build_info")},
				{labels.MustNewMatcher(labels.MatchEqual, "job", "prometheus")},
				{labels.MustNewMatcher(labels.MatchRegexp, "__name__", ".+")},
			}
			for i, matchers := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.GetValue(context.TODO(), model.Time(0).Time(), model.Time(10).Time(), matchers)
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[droplabel]; ok {
								t.Fatalf("found droplabel in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})
	})

	t.Run("replace", func(t *testing.T) {
		const sourcelabel = "job"
		const targetlabel = "scrape_job"

		cfgs := []*MetricRelabelConfig{{SourceLabel: sourcelabel, TargetLabel: targetlabel, Action: "replace"}}
		c, err := NewMetricsRelabelClient(client, cfgs)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("LabelNames", func(t *testing.T) {
			// Basic LabelNames
			tests := [][]string{
				nil,
				{"prometheus_build_info"},
				{`{scrape_job!=""}`},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelNames(context.TODO(), test, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Contains(lbls, targetlabel) {
						t.Fatalf("LabelNames does not contain targetlabel label: %v", lbls)
					}
				})
			}
		})

		t.Run("LabelValues", func(t *testing.T) {
			tests := []struct {
				label    string
				matchers []string
			}{
				{
					label:    "job",
					matchers: nil,
				},
				{
					label:    "job",
					matchers: []string{`{job="prometheus"}`},
				},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelValues(context.TODO(), test.label, test.matchers, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}

					values := make([]string, len(lbls))
					for i, l := range lbls {
						values[i] = string(l)
					}
					// TODO: how to test?
				})
			}
		})

		t.Run("Query", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info", // Test that we don't get a label back
				`{job="prometheus"}`,    // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, sourcelabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.Query(context.TODO(), query, model.Time(5).Time())
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Vector:
						for _, v := range []*model.Sample(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("sourcelabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("QueryRange", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info",     // Test that we don't get a label back
				`{scrape_job="prometheus"}`, // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, targetlabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.QueryRange(context.TODO(), query, v1.Range{Start: model.Time(0).Time(), End: model.Time(10).Time(), Step: time.Duration(1e6)})
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("Series", func(t *testing.T) {
			tests := [][]string{
				{`prometheus_build_info`},
				{`{job="prometheus"}`},
				{`{__name__=~".+"}`},
				{`{job="prometheus"}`, `{version="2.27.0"}`},
			}
			for i, matches := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lblsets, _, err := c.Series(context.TODO(), matches, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					for _, lblset := range lblsets {
						if _, ok := lblset[model.LabelName(targetlabel)]; !ok {
							t.Fatalf("targetlabel not found in response")
						}
					}
				})
			}
		})

		t.Run("GetValue", func(t *testing.T) {
			tests := [][]*labels.Matcher{
				{labels.MustNewMatcher(labels.MatchEqual, "__name__", "prometheus_build_info")},
				{labels.MustNewMatcher(labels.MatchEqual, "job", "prometheus")},
				{labels.MustNewMatcher(labels.MatchRegexp, "__name__", ".+")},
			}
			for i, matchers := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.GetValue(context.TODO(), model.Time(0).Time(), model.Time(10).Time(), matchers)
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})
	})

	t.Run("lowercase", func(t *testing.T) {
		const sourcelabel = "branch"
		const targetlabel = "lowerBranch"

		cfgs := []*MetricRelabelConfig{{SourceLabel: sourcelabel, TargetLabel: targetlabel, Action: "lowercase"}}
		c, err := NewMetricsRelabelClient(client, cfgs)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("LabelNames", func(t *testing.T) {
			// Basic LabelNames
			tests := [][]string{
				nil,
				{"prometheus_build_info"},
				{`{lowerBranch!=""}`},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelNames(context.TODO(), test, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Contains(lbls, targetlabel) {
						t.Fatalf("LabelNames does not contain targetlabel label: %v", lbls)
					}
				})
			}
		})

		t.Run("LabelValues", func(t *testing.T) {
			tests := []struct {
				label    string
				matchers []string
			}{
				{
					label:    "job",
					matchers: nil,
				},
				{
					label:    "job",
					matchers: []string{`{job="prometheus"}`},
				},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelValues(context.TODO(), test.label, test.matchers, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}

					values := make([]string, len(lbls))
					for i, l := range lbls {
						values[i] = string(l)
					}
					// TODO: how to test?
				})
			}
		})

		t.Run("Query", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info", // Test that we don't get a label back
				`{job="prometheus"}`,    // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, sourcelabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.Query(context.TODO(), query, model.Time(5).Time())
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Vector:
						for _, v := range []*model.Sample(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("sourcelabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("QueryRange", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info",     // Test that we don't get a label back
				`{scrape_job="prometheus"}`, // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, targetlabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.QueryRange(context.TODO(), query, v1.Range{Start: model.Time(0).Time(), End: model.Time(10).Time(), Step: time.Duration(1e6)})
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("Series", func(t *testing.T) {
			tests := [][]string{
				{`prometheus_build_info`},
				{`{job="prometheus"}`},
				{`{__name__=~".+"}`},
				{`{job="prometheus"}`, `{version="2.27.0"}`},
			}
			for i, matches := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lblsets, _, err := c.Series(context.TODO(), matches, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					for _, lblset := range lblsets {
						if _, ok := lblset[model.LabelName(targetlabel)]; !ok {
							t.Fatalf("targetlabel not found in response")
						}
					}
				})
			}
		})

		t.Run("GetValue", func(t *testing.T) {
			tests := [][]*labels.Matcher{
				{labels.MustNewMatcher(labels.MatchEqual, "__name__", "prometheus_build_info")},
				{labels.MustNewMatcher(labels.MatchEqual, "job", "prometheus")},
				{labels.MustNewMatcher(labels.MatchRegexp, "__name__", ".+")},
			}
			for i, matchers := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.GetValue(context.TODO(), model.Time(0).Time(), model.Time(10).Time(), matchers)
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})
	})

	t.Run("uppercase", func(t *testing.T) {
		const sourcelabel = "branch"
		const targetlabel = "lowerBranch"

		cfgs := []*MetricRelabelConfig{{SourceLabel: sourcelabel, TargetLabel: targetlabel, Action: "uppercase"}}
		c, err := NewMetricsRelabelClient(client, cfgs)
		if err != nil {
			t.Fatal(err)
		}

		t.Run("LabelNames", func(t *testing.T) {
			// Basic LabelNames
			tests := [][]string{
				nil,
				{"prometheus_build_info"},
				{`{lowerBranch!=""}`},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelNames(context.TODO(), test, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					if !slices.Contains(lbls, targetlabel) {
						t.Fatalf("LabelNames does not contain targetlabel label: %v", lbls)
					}
				})
			}
		})

		t.Run("LabelValues", func(t *testing.T) {
			tests := []struct {
				label    string
				matchers []string
			}{
				{
					label:    "job",
					matchers: nil,
				},
				{
					label:    "job",
					matchers: []string{`{job="prometheus"}`},
				},
			}
			for i, test := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lbls, _, err := c.LabelValues(context.TODO(), test.label, test.matchers, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}

					values := make([]string, len(lbls))
					for i, l := range lbls {
						values[i] = string(l)
					}
					// TODO: how to test?
				})
			}
		})

		t.Run("Query", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info", // Test that we don't get a label back
				`{job="prometheus"}`,    // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, sourcelabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.Query(context.TODO(), query, model.Time(5).Time())
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Vector:
						for _, v := range []*model.Sample(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("sourcelabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("QueryRange", func(t *testing.T) {
			tests := []string{
				"prometheus_build_info",     // Test that we don't get a label back
				`{scrape_job="prometheus"}`, // Test other matcher to make sure we don't get a label back
				`{__name__=~".+"}`,
				fmt.Sprintf(`sum(prometheus_build_info) by (%s)`, targetlabel), // Do a sum on the
			}
			for i, query := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.QueryRange(context.TODO(), query, v1.Range{Start: model.Time(0).Time(), End: model.Time(10).Time(), Step: time.Duration(1e6)})
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})

		t.Run("Series", func(t *testing.T) {
			tests := [][]string{
				{`prometheus_build_info`},
				{`{job="prometheus"}`},
				{`{__name__=~".+"}`},
				{`{job="prometheus"}`, `{version="2.27.0"}`},
			}
			for i, matches := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					lblsets, _, err := c.Series(context.TODO(), matches, model.Time(0).Time(), model.Time(10).Time())
					if err != nil {
						t.Fatal(err)
					}
					for _, lblset := range lblsets {
						if _, ok := lblset[model.LabelName(targetlabel)]; !ok {
							t.Fatalf("targetlabel not found in response")
						}
					}
				})
			}
		})

		t.Run("GetValue", func(t *testing.T) {
			tests := [][]*labels.Matcher{
				{labels.MustNewMatcher(labels.MatchEqual, "__name__", "prometheus_build_info")},
				{labels.MustNewMatcher(labels.MatchEqual, "job", "prometheus")},
				{labels.MustNewMatcher(labels.MatchRegexp, "__name__", ".+")},
			}
			for i, matchers := range tests {
				t.Run(strconv.Itoa(i), func(t *testing.T) {
					val, _, err := c.GetValue(context.TODO(), model.Time(0).Time(), model.Time(10).Time(), matchers)
					if err != nil {
						t.Fatal(err)
					}
					switch valTyped := val.(type) {
					case model.Matrix:
						for _, v := range []*model.SampleStream(valTyped) {
							if _, ok := v.Metric[targetlabel]; !ok {
								t.Fatalf("targetlabel not found in response")
							}
						}
					default:
						t.Fatalf("unknown val type: %T", val)
					}
				})
			}
		})
	})

	// TODO: compounds?
}
