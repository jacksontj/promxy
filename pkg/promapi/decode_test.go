package promapi

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func dumpSeriesSet(t *testing.T, ss storage.SeriesSet) map[string]string {
	t.Helper()
	out := map[string]string{}
	for ss.Next() {
		s := ss.At()
		out[s.Labels().String()] = dumpSamples(s.Iterator(nil))
	}
	if err := ss.Err(); err != nil {
		t.Fatalf("seriesset err: %v", err)
	}
	return out
}

func dumpSamples(it chunkenc.Iterator) string {
	var b strings.Builder
	for {
		switch it.Next() {
		case chunkenc.ValNone:
			return b.String()
		case chunkenc.ValFloat:
			ts, v := it.At()
			fmt.Fprintf(&b, "%d=%s ", ts, strconv.FormatFloat(v, 'g', -1, 64))
		case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
			ts, fh := it.AtFloatHistogram(nil)
			fmt.Fprintf(&b, "%d=H{c=%g,s=%g,cv=%v,pb=%v} ", ts, fh.Count, fh.Sum, fh.CustomValues, fh.PositiveBuckets)
		}
	}
}

// TestDecodeSeriesSet checks the decoder against hand-written expectations for
// vector/matrix/scalar, special floats, and native histograms.
func TestDecodeSeriesSet(t *testing.T) {
	cases := []struct {
		name string
		body string
		want map[string]string
	}{
		{
			name: "vector",
			body: `{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":{"__name__":"up","job":"a"},"value":[100.000,"1"]},` +
				`{"metric":{"__name__":"up","job":"b"},"value":[100.500,"0"]}` +
				`]},"warnings":[],"infos":[]}`,
			want: map[string]string{
				`{__name__="up", job="a"}`: "100000=1 ",
				`{__name__="up", job="b"}`: "100500=0 ",
			},
		},
		{
			name: "matrix_and_specials",
			body: `{"status":"success","data":{"resultType":"matrix","result":[` +
				`{"metric":{"__name__":"m"},"values":[[100.000,"1"],[160.000,"NaN"]]},` +
				`{"metric":{"__name__":"n"},"values":[[100.000,"+Inf"]]}` +
				`]}}`,
			want: map[string]string{
				`{__name__="m"}`: "100000=1 160000=NaN ",
				`{__name__="n"}`: "100000=+Inf ",
			},
		},
		{
			name: "scalar",
			body: `{"status":"success","data":{"resultType":"scalar","result":[100.000,"42"]}}`,
			want: map[string]string{"{}": "100000=42 "},
		},
		{
			name: "histogram",
			body: `{"status":"success","data":{"resultType":"vector","result":[` +
				`{"metric":{"__name__":"h"},"histogram":[100.000,{"count":"3","sum":"6","buckets":[[0,"0","2","3"]]}]}` +
				`]}}`,
			want: map[string]string{`{__name__="h"}`: "100000=H{c=3,s=6,cv=[2],pb=[3]} "},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dumpSeriesSet(t, DecodeSeriesSet([]byte(tc.body)))
			if len(got) != len(tc.want) {
				t.Fatalf("series count: got %d want %d (%v)", len(got), len(tc.want), got)
			}
			for k, w := range tc.want {
				if got[k] != w {
					t.Fatalf("series %s: got %q want %q", k, got[k], w)
				}
			}
		})
	}
}

func TestDecodeSeriesSetError(t *testing.T) {
	ss := DecodeSeriesSet([]byte(`{"status":"error","errorType":"bad_data","error":"boom"}`))
	if ss.Err() == nil || ss.Err().Error() != "bad_data: boom" {
		t.Fatalf("expected ResponseError, got %v", ss.Err())
	}
}

func buildMatrixBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"status":"success","data":{"resultType":"matrix","result":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"metric":{"__name__":"m","instance":"inst-%d","job":"bench","series":"%d"},"values":[[%d.000,"%d"],[%d.000,"%d"]]}`,
			i%500, i, 1700000000+i, i, 1700000060+i, i*2)
	}
	b.WriteString(`]},"warnings":[],"infos":[]}`)
	return []byte(b.String())
}

func BenchmarkDecodeSeriesSet(b *testing.B) {
	body := buildMatrixBody(5000)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ss := DecodeSeriesSet(body)
		for ss.Next() {
			_ = ss.At().Labels()
		}
	}
}
