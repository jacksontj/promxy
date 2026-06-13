package expfmt

import (
	"bytes"
	"math"
	"testing"

	dto "github.com/prometheus/client_model/go"
	commonexpfmt "github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

func sp(s string) *string   { return &s }
func fp(f float64) *float64 { return &f }
func ip(i int64) *int64     { return &i }

func untypedMetric(value float64, ts *int64, lbls ...string) *dto.Metric {
	m := &dto.Metric{Untyped: &dto.Untyped{Value: fp(value)}, TimestampMs: ts}
	for i := 0; i+1 < len(lbls); i += 2 {
		m.Label = append(m.Label, &dto.LabelPair{Name: sp(lbls[i]), Value: sp(lbls[i+1])})
	}
	return m
}

func untypedFamily(name string, metrics ...*dto.Metric) *dto.MetricFamily {
	return &dto.MetricFamily{
		Name:   sp(name),
		Type:   dto.MetricType_UNTYPED.Enum(),
		Metric: metrics,
	}
}

// TestMetricFamilyToTextMatchesCommon asserts our MetricFamilyToText is
// byte-for-byte identical to common/expfmt's (bare, unescaped) MetricFamilyToText
// -- which quotes non-legacy names -- across UTF-8 names, value escaping, and
// special floats. This validates the shared encoding primitives.
func TestMetricFamilyToTextMatchesCommon(t *testing.T) {
	cases := map[string]*dto.MetricFamily{
		"legacy": untypedFamily("http_requests_total",
			untypedMetric(1234, ip(1700000000000), "method", "get", "code", "200"),
			untypedMetric(5, ip(1700000000000), "method", "post", "code", "500"),
		),
		"no_labels":      untypedFamily("up", untypedMetric(1, ip(1700000000000))),
		"no_timestamp":   untypedFamily("up", untypedMetric(1, nil, "job", "x")),
		"colon_name":     untypedFamily("a:b:rate", untypedMetric(2.5, nil, "x", "y")),
		"empty_value":    untypedFamily("m", untypedMetric(1, nil, "present", "", "other", "v")),
		"utf8_metric":    untypedFamily("my.metric.name", untypedMetric(7, ip(123), "a", "b")),
		"utf8_label":     untypedFamily("metric", untypedMetric(7, nil, "label.with.dots", "v")),
		"utf8_both":      untypedFamily("föö.bar", untypedMetric(7, nil, "münchen", "naïve")),
		"utf8_no_labels": untypedFamily("my.metric", untypedMetric(7, nil)),
		"escape_value":   untypedFamily("m", untypedMetric(1, nil, "l", "a\\b\nc\"d")),
		"escape_in_name": untypedFamily(`weird"name`, untypedMetric(1, nil, "l", "v")),
		"special_floats": untypedFamily("m",
			untypedMetric(0, nil, "k", "zero"),
			untypedMetric(1, nil, "k", "one"),
			untypedMetric(-1, nil, "k", "negone"),
			untypedMetric(math.NaN(), nil, "k", "nan"),
			untypedMetric(math.Inf(1), nil, "k", "posinf"),
			untypedMetric(math.Inf(-1), nil, "k", "neginf"),
			untypedMetric(3.141592653589793, nil, "k", "pi"),
			untypedMetric(-2.5e-10, nil, "k", "small"),
			untypedMetric(1234567890123, nil, "k", "big"),
		),
	}

	for name, mf := range cases {
		t.Run(name, func(t *testing.T) {
			var want bytes.Buffer
			if _, err := commonexpfmt.MetricFamilyToText(&want, mf); err != nil {
				t.Fatalf("common MetricFamilyToText: %v", err)
			}
			var got bytes.Buffer
			if err := MetricFamilyToText(&got, mf); err != nil {
				t.Fatalf("MetricFamilyToText: %v", err)
			}
			if got.String() != want.String() {
				t.Fatalf("mismatch\n got: %q\nwant: %q", got.String(), want.String())
			}
		})
	}
}

// seriesToDTOMetric builds a dto.Metric from a series the same way the federation
// Encoder writes it: series labels (sorted, minus __name__ and empty values),
// then external labels not already present, in order.
func seriesToDTOMetric(lbls labels.Labels, external []labels.Label, v float64, ts *int64) *dto.Metric {
	m := &dto.Metric{Untyped: &dto.Untyped{Value: fp(v)}, TimestampMs: ts}
	lbls.Range(func(l labels.Label) {
		if l.Name == labels.MetricName || l.Value == "" {
			return
		}
		m.Label = append(m.Label, &dto.LabelPair{Name: sp(l.Name), Value: sp(l.Value)})
	})
	for _, el := range external {
		if lbls.Get(el.Name) == "" {
			m.Label = append(m.Label, &dto.LabelPair{Name: sp(el.Name), Value: sp(el.Value)})
		}
	}
	return m
}

// TestEncoderMatchesCommonAcrossSchemes asserts the streaming Encoder is
// byte-for-byte identical to common/expfmt's encoder path
// (MetricFamilyToText(EscapeMetricFamily(mf, scheme))) for both the default
// underscore-escaping scheme and the allow-utf-8 (quoting) scheme. This is the
// correctness guarantee for the lean /federate handler, including UTF-8 names.
func TestEncoderMatchesCommonAcrossSchemes(t *testing.T) {
	type series struct {
		lbls     labels.Labels
		external []labels.Label
		v        float64
		t        int64
	}
	external := []labels.Label{{Name: "region", Value: "us-east"}, {Name: "src", Value: "promxy"}}

	groups := map[string][]series{
		"legacy": {
			{lbls: labels.FromStrings("__name__", "http_requests_total", "code", "200", "method", "get"), external: external, v: 1234, t: 1700000000000},
			{lbls: labels.FromStrings("__name__", "http_requests_total", "code", "500", "method", "post", "region", "eu"), external: external, v: 5, t: 1700000000000},
		},
		"utf8_names": {
			{lbls: labels.FromStrings("__name__", "my.metric", "label.dots", "v", "ok", "1"), external: external, v: 7, t: 123},
			{lbls: labels.FromStrings("__name__", "my.metric", "münchen", "naïve"), external: external, v: math.NaN(), t: 456},
		},
		"special_and_escape": {
			{lbls: labels.FromStrings("__name__", "m", "l", "a\\b\nc\"d"), v: math.Inf(1), t: 1},
			{lbls: labels.FromStrings("__name__", "m", "l", "x"), v: 0, t: 2},
		},
	}

	schemes := map[string]model.EscapingScheme{
		"underscore": model.UnderscoreEscaping,
		"utf8":       model.NoEscaping,
	}

	for gname, ss := range groups {
		for sname, scheme := range schemes {
			t.Run(gname+"/"+sname, func(t *testing.T) {
				// Reference: build the dto family (one metric name per group),
				// escape per scheme, encode with common's bare MetricFamilyToText
				// -- exactly common/expfmt's NewEncoder text path.
				name := ss[0].lbls.Get(labels.MetricName)
				mf := &dto.MetricFamily{Name: sp(name), Type: dto.MetricType_UNTYPED.Enum()}
				for _, s := range ss {
					mf.Metric = append(mf.Metric, seriesToDTOMetric(s.lbls, s.external, s.v, ip(s.t)))
				}
				var want bytes.Buffer
				if _, err := commonexpfmt.MetricFamilyToText(&want, model.EscapeMetricFamily(mf, scheme)); err != nil {
					t.Fatalf("common encode: %v", err)
				}

				// Ours: stream each series through the Encoder.
				var got bytes.Buffer
				enc := NewEncoder(&got, scheme)
				for _, s := range ss {
					enc.WriteFloatSample(s.lbls.Get(labels.MetricName), s.lbls, s.external, s.v, s.t)
				}
				if err := enc.Flush(); err != nil {
					t.Fatalf("flush: %v", err)
				}

				if got.String() != want.String() {
					t.Fatalf("mismatch\n got: %q\nwant: %q", got.String(), want.String())
				}
			})
		}
	}
}
