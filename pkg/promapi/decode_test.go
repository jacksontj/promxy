package promapi

import (
	stdjson "encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/util/annotations"
)

// TestDecodeAnnotationClassification asserts that downstream warning/info
// strings are re-wrapped with the typed annotation sentinels so consumers can
// classify them via errors.Is -- the regression that produced "unexpected
// annotation type" failures on offset queries.
func TestDecodeAnnotationClassification(t *testing.T) {
	body := []byte(`{"status":"success","data":{"resultType":"vector","result":[]},` +
		`"warnings":["PromQL warning: something is off"],` +
		`"infos":["PromQL info: metric might not be a counter"]}`)

	ss := DecodeSeriesSet(body)
	if ss.Err() != nil {
		t.Fatalf("unexpected error: %v", ss.Err())
	}

	errs := ss.Warnings().AsErrors()
	if len(errs) != 2 {
		t.Fatalf("expected 2 annotations, got %d: %v", len(errs), errs)
	}
	var sawWarn, sawInfo bool
	for _, e := range errs {
		if errors.Is(e, annotations.PromQLWarning) {
			sawWarn = true
		}
		if errors.Is(e, annotations.PromQLInfo) {
			sawInfo = true
		}
	}
	if !sawWarn {
		t.Error("warning was not classified as annotations.PromQLWarning")
	}
	if !sawInfo {
		t.Error("info was not classified as annotations.PromQLInfo")
	}
}

// TestDecodeEdgeResultTypes covers the result shapes outside the common
// vector/matrix path: empty results, string results (no series), and an
// unknown resultType (which must surface as an error).
func TestDecodeEdgeResultTypes(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		wantSeries int
		wantErr    bool
	}{
		{"empty_matrix", `{"status":"success","data":{"resultType":"matrix","result":[]}}`, 0, false},
		{"empty_vector", `{"status":"success","data":{"resultType":"vector","result":[]}}`, 0, false},
		{"string", `{"status":"success","data":{"resultType":"string","result":[1700000000,"hello"]}}`, 0, false},
		{"unknown_type", `{"status":"success","data":{"resultType":"bogus","result":[{"metric":{},"value":[0,"1"]}]}}`, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ss := DecodeSeriesSet([]byte(tc.body))
			n := 0
			for ss.Next() {
				n++
			}
			if (ss.Err() != nil) != tc.wantErr {
				t.Fatalf("err mismatch: got %v want error=%v", ss.Err(), tc.wantErr)
			}
			if n != tc.wantSeries {
				t.Fatalf("series count: got %d want %d", n, tc.wantSeries)
			}
		})
	}
}

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

// envelope/innerResult mirror the shape the pre-refactor query path decoded with
// stdlib encoding/json (promclient.queryWithInfos): outer
// {status,data,warnings,infos}, then data {resultType,result}, then result into
// a model.Matrix (Vector/Matrix with model.Metric maps + checkValid).
type envelope struct {
	Status string             `json:"status"`
	Data   stdjson.RawMessage `json:"data"`
}

type innerResult struct {
	Type   model.ValueType    `json:"resultType"`
	Result stdjson.RawMessage `json:"result"`
}

func decodeModelValueStdlib(body []byte) (model.Matrix, error) {
	var ar envelope
	if err := stdjson.Unmarshal(body, &ar); err != nil {
		return nil, err
	}
	var ir innerResult
	if err := stdjson.Unmarshal(ar.Data, &ir); err != nil {
		return nil, err
	}
	var mv model.Matrix
	if err := stdjson.Unmarshal(ir.Result, &mv); err != nil {
		return nil, err
	}
	return mv, nil
}

// BenchmarkDecodeModelValueStdlib is the pre-refactor baseline: stdlib decode of
// the same body into a model.Matrix. Compare against BenchmarkDecodeSeriesSet to
// see what dropping model.Value for a streaming SeriesSet decode buys on the
// query/federate hot path.
func BenchmarkDecodeModelValueStdlib(b *testing.B) {
	body := buildMatrixBody(5000)
	mv, err := decodeModelValueStdlib(body)
	if err != nil {
		b.Fatal(err)
	}
	if len(mv) != 5000 {
		b.Fatalf("expected 5000 series, got %d", len(mv))
	}
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := decodeModelValueStdlib(body); err != nil {
			b.Fatal(err)
		}
	}
}
