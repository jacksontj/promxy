package promclient

import (
	stdjson "encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// chunkIter abstracts the two iteration shapes: the new storage.Series and the
// old *SeriesIterator (which is itself a chunkenc.Iterator).
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
		vt := it.Next()
		switch vt {
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

// dumpModelValuePath decodes the body the *current* way: stdlib into model.Value
// (via queryResult), then IteratorsForValue -> *SeriesIterator.
func dumpModelValuePath(t *testing.T, body []byte) map[string]string {
	t.Helper()
	var ar struct {
		Status string             `json:"status"`
		Data   stdjson.RawMessage `json:"data"`
	}
	if err := stdjson.Unmarshal(body, &ar); err != nil {
		t.Fatalf("envelope: %v", err)
	}
	var qr queryResult
	if err := stdjson.Unmarshal(ar.Data, &qr); err != nil {
		t.Fatalf("queryResult: %v", err)
	}
	out := map[string]string{}
	for _, it := range IteratorsForValue(qr.v) {
		out[it.Labels().String()] = dumpSamples(it)
	}
	return out
}

func buildVectorBody(n int) []byte {
	var b strings.Builder
	b.WriteString(`{"status":"success","data":{"resultType":"vector","result":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, `{"metric":{"__name__":"m","instance":"inst-%d","job":"bench","series":"%d"},"value":[%d.000,"%d"]}`,
			i%500, i, 1700000000+i, i)
	}
	b.WriteString(`]},"warnings":[],"infos":[]}`)
	return []byte(b.String())
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

// TestDecodeSeriesSetMatchesModelValue asserts the streaming decoder produces
// the same series/labels/samples as the current model.Value path.
func TestDecodeSeriesSetMatchesModelValue(t *testing.T) {
	bodies := map[string][]byte{
		"vector": buildVectorBody(200),
		"matrix": buildMatrixBody(200),
		"special_values": []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"__name__":"a"},"value":[100.000,"NaN"]},` +
			`{"metric":{"__name__":"b"},"value":[100.000,"+Inf"]},` +
			`{"metric":{"__name__":"c","x":"y"},"value":[100.500,"-Inf"]},` +
			`{"metric":{"__name__":"d"},"value":[100.000,"3.14159"]}` +
			`]},"warnings":[],"infos":[]}`),
		"histogram_vector": []byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"__name__":"h","kind":"native"},"histogram":[100.000,{"count":"10","sum":"42.5","buckets":[[0,"0","1","3"],[0,"1","2","7"]]}]}` +
			`]},"warnings":[],"infos":[]}`),
		"histogram_matrix": []byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"h"},"histograms":[[100.000,{"count":"3","sum":"6","buckets":[[0,"0","2","3"]]}],[160.000,{"count":"5","sum":"9","buckets":[[0,"0","2","5"]]}]]}` +
			`]},"warnings":[],"infos":[]}`),
	}
	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			want := dumpModelValuePath(t, body)
			got := dumpSeriesSet(t, DecodeSeriesSet(body))
			if len(got) != len(want) {
				t.Fatalf("series count: got %d want %d", len(got), len(want))
			}
			keys := make([]string, 0, len(want))
			for k := range want {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				if got[k] != want[k] {
					t.Fatalf("series %s mismatch\n got: %q\nwant: %q", k, got[k], want[k])
				}
			}
		})
	}
}

// --- benchmark: new streaming decode vs current model.Value + IteratorsForValue ---

func BenchmarkDecodeModelValuePath(b *testing.B) {
	body := buildMatrixBody(5000)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var ar struct {
			Data stdjson.RawMessage `json:"data"`
		}
		_ = stdjson.Unmarshal(body, &ar)
		var qr queryResult
		_ = stdjson.Unmarshal(ar.Data, &qr)
		its := IteratorsForValue(qr.v)
		var sink labels.Labels
		for _, it := range its {
			sink = it.Labels()
		}
		_ = sink
	}
}

func BenchmarkDecodeSeriesSet(b *testing.B) {
	body := buildMatrixBody(5000)
	b.SetBytes(int64(len(body)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ss := DecodeSeriesSet(body)
		var sink labels.Labels
		for ss.Next() {
			sink = ss.At().Labels()
		}
		_ = sink
	}
}
