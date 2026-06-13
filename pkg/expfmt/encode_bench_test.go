package expfmt

import (
	"io"
	"strconv"
	"testing"

	dto "github.com/prometheus/client_model/go"
	commonexpfmt "github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

type benchSample struct {
	lbls labels.Labels
	v    float64
	t    int64
}

func benchSeries(n int) []benchSample {
	out := make([]benchSample, n)
	for i := 0; i < n; i++ {
		out[i] = benchSample{
			lbls: labels.FromStrings(
				labels.MetricName, "promxy_bench_metric",
				"instance", "inst-"+strconv.Itoa(i%500),
				"job", "bench",
				"region", "us-east-1",
				"series", strconv.Itoa(i),
			),
			v: float64(i),
			t: int64(1700000000000 + i),
		}
	}
	return out
}

// BenchmarkEncoderCommon is the upstream cost per /federate request: build a
// dto.MetricFamily tree, then encode via common/expfmt.
func BenchmarkEncoderCommon(b *testing.B) {
	ss := benchSeries(5000)
	format := commonexpfmt.NewFormat(commonexpfmt.TypeTextPlain)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		mf := &dto.MetricFamily{Name: sp("promxy_bench_metric"), Type: dto.MetricType_UNTYPED.Enum()}
		mf.Metric = make([]*dto.Metric, 0, len(ss))
		for _, s := range ss {
			mf.Metric = append(mf.Metric, seriesToDTOMetric(s.lbls, nil, s.v, ip(s.t)))
		}
		enc := commonexpfmt.NewEncoder(io.Discard, format)
		if err := enc.Encode(mf); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncoderLean is the promxy path: stream straight from labels, no dto.
func BenchmarkEncoderLean(b *testing.B) {
	ss := benchSeries(5000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		enc := NewEncoder(io.Discard, model.UnderscoreEscaping)
		for _, s := range ss {
			enc.WriteFloatSample(s.lbls.Get(labels.MetricName), s.lbls, nil, s.v, s.t)
		}
		if err := enc.Flush(); err != nil {
			b.Fatal(err)
		}
	}
}
