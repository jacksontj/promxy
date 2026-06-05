package promapi

import (
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	jsoniter "github.com/json-iterator/go"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"
)

// annotationPosSuffix matches the " (line:col)" suffix that upstream's
// annotations.AsStrings appends to formatted annotation messages. The position
// refers to the upstream query's text, which is meaningless to promxy's callers
// (we never round-trip the original query string). Stripping it also lets the
// upstream promql test framework match expected warning text exactly.
var annotationPosSuffix = regexp.MustCompile(` \(\d+:\d+\)$`)

// toAnnotationError re-wraps a downstream warning/info string back into a
// properly-typed annotation. The v1 JSON API serializes annotations as plain
// strings (losing the typed wrapping), prefixed with "PromQL warning: " /
// "PromQL info: "; we detect the prefix and re-wrap with the matching sentinel
// so consumers can classify info-vs-warning via errors.Is. The trailing
// " (line:col)" position suffix is stripped — it points into the downstream's
// query text, which promxy never round-trips.
func toAnnotationError(s string) error {
	s = annotationPosSuffix.ReplaceAllString(s, "")
	if rest, ok := strings.CutPrefix(s, "PromQL warning: "); ok {
		return fmt.Errorf("%w: %s", annotations.PromQLWarning, rest)
	}
	if rest, ok := strings.CutPrefix(s, "PromQL info: "); ok {
		return fmt.Errorf("%w: %s", annotations.PromQLInfo, rest)
	}
	return errors.New(s)
}

// ResponseError is a downstream API error (a status:"error" body or a malformed
// response). It carries the API errorType and message but, unlike
// client_golang's v1.Error, adds no client_golang dependency -- keeping this
// decoder a standalone, reusable unit (and a step toward dropping client_golang
// entirely).
type ResponseError struct {
	Type string
	Msg  string
}

func (e *ResponseError) Error() string {
	if e.Type != "" {
		return e.Type + ": " + e.Msg
	}
	return e.Msg
}

// This is the replacement for the model.Value decode path (queryWithInfos +
// IteratorsForValue + metricToLabels). It streams a Prometheus HTTP API
// response straight into a storage.SeriesSet: each series' metric is read
// directly into a labels.Labels (no model.Metric map) and samples (float or
// native histogram) are read without model.SamplePair. Warnings and infos are
// returned as annotations.Annotations carried by the SeriesSet.

var jsonCfg = jsoniter.ConfigCompatibleWithStandardLibrary

// floatSample is a chunks.Sample backed by a single float point.
type floatSample struct {
	t int64
	f float64
}

func (s floatSample) T() int64                      { return s.t }
func (s floatSample) F() float64                    { return s.f }
func (s floatSample) H() *histogram.Histogram       { return nil }
func (s floatSample) FH() *histogram.FloatHistogram { return nil }
func (s floatSample) Type() chunkenc.ValueType      { return chunkenc.ValFloat }
func (s floatSample) Copy() chunks.Sample           { return s }

// histSample is a chunks.Sample backed by a native (float) histogram point.
type histSample struct {
	t  int64
	fh *histogram.FloatHistogram
}

func (s histSample) T() int64                      { return s.t }
func (s histSample) F() float64                    { return 0 }
func (s histSample) H() *histogram.Histogram       { return nil }
func (s histSample) FH() *histogram.FloatHistogram { return s.fh }
func (s histSample) Type() chunkenc.ValueType      { return chunkenc.ValFloatHistogram }
func (s histSample) Copy() chunks.Sample           { return histSample{s.t, s.fh.Copy()} }

// sampleSlice adapts []chunks.Sample to storage.Samples.
type sampleSlice []chunks.Sample

func (s sampleSlice) Get(i int) chunks.Sample { return s[i] }
func (s sampleSlice) Len() int                { return len(s) }

// NewSeries builds a storage.Series whose float-histogram reads are defensively
// copied (via storage.NewListSeriesIteratorWithCopy). The plain
// storage.NewListSeries hands back the stored *FloatHistogram pointer, so a
// caller (e.g. the PromQL engine) that reuses a hint across samples can observe
// aliased histograms when the same histogram repeats; copy-on-read prevents that
// while leaving float reads zero-copy.
func NewSeries(lbls labels.Labels, samples []chunks.Sample) storage.Series {
	return &storage.SeriesEntry{
		Lset: lbls,
		SampleIteratorFn: func(chunkenc.Iterator) chunkenc.Iterator {
			return storage.NewListSeriesIteratorWithCopy(sampleSlice(samples))
		},
	}
}

// SeriesSet is a minimal in-memory storage.SeriesSet carrying series, warnings
// and an error. It is the one reusable list-SeriesSet for promxy.
//
// TODO(seriesset-refactor): pkg/proxyquerier has an identical type; once the
// query path returns this directly, delete proxyquerier's copy and use this.
type SeriesSet struct {
	series   []storage.Series
	idx      int
	warnings annotations.Annotations
	err      error
}

// NewSeriesSet returns a storage.SeriesSet over the given series.
func NewSeriesSet(series []storage.Series, warnings annotations.Annotations, err error) *SeriesSet {
	return &SeriesSet{series: series, idx: -1, warnings: warnings, err: err}
}

func (s *SeriesSet) Next() bool                        { s.idx++; return s.idx < len(s.series) }
func (s *SeriesSet) At() storage.Series                { return s.series[s.idx] }
func (s *SeriesSet) Err() error                        { return s.err }
func (s *SeriesSet) Warnings() annotations.Annotations { return s.warnings }

// DecodeSeriesSet streams an API response body into a storage.SeriesSet.
func DecodeSeriesSet(body []byte) storage.SeriesSet {
	iter := jsonCfg.BorrowIterator(body)
	defer jsonCfg.ReturnIterator(iter)

	var (
		status      string
		errType     string
		errMsg      string
		resultType  string
		resultBytes []byte
		anns        annotations.Annotations
	)

	for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
		switch key {
		case "status":
			status = iter.ReadString()
		case "errorType":
			errType = iter.ReadString()
		case "error":
			errMsg = iter.ReadString()
		case "warnings", "infos":
			// Downstream warnings/infos already carry their
			// "PromQL warning: " / "PromQL info: " prefixes; preserve them as
			// annotations so SeriesSet.Warnings() exposes both.
			for iter.ReadArray() {
				anns = anns.Add(toAnnotationError(iter.ReadString()))
			}
		case "data":
			for dk := iter.ReadObject(); dk != ""; dk = iter.ReadObject() {
				switch dk {
				case "resultType":
					resultType = iter.ReadString()
				case "result":
					// Capture raw so we can decode once resultType is known
					// (the two fields are ordered resultType-then-result in
					// practice, but don't rely on it).
					resultBytes = iter.SkipAndReturnBytes()
				default:
					iter.Skip()
				}
			}
		default:
			iter.Skip()
		}
	}

	if iter.Error != nil && !errors.Is(iter.Error, io.EOF) {
		return NewSeriesSet(nil, anns, &ResponseError{Type: "bad_response", Msg: iter.Error.Error()})
	}
	if status == "error" {
		return NewSeriesSet(nil, anns, &ResponseError{Type: errType, Msg: errMsg})
	}

	series, err := decodeResult(resultType, resultBytes)
	if err != nil {
		return NewSeriesSet(nil, anns, err)
	}
	return NewSeriesSet(series, anns, nil)
}

func decodeResult(resultType string, body []byte) ([]storage.Series, error) {
	if len(body) == 0 {
		return nil, nil
	}
	iter := jsonCfg.BorrowIterator(body)
	defer jsonCfg.ReturnIterator(iter)

	switch resultType {
	case "vector":
		return decodeVector(iter), iter.Error
	case "matrix":
		return decodeMatrix(iter), iter.Error
	case "scalar":
		t, f := decodeSamplePair(iter)
		return []storage.Series{NewSeries(labels.EmptyLabels(), []chunks.Sample{floatSample{t, f}})}, iter.Error
	case "string":
		// strings carry no series; the model.Value path didn't support them either.
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown result type %q", resultType)
	}
}

func decodeVector(iter *jsoniter.Iterator) []storage.Series {
	var out []storage.Series
	var b labels.ScratchBuilder
	for iter.ReadArray() {
		b.Reset()
		var sample chunks.Sample
		for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
			switch k {
			case "metric":
				readMetric(iter, &b)
			case "value":
				t, f := decodeSamplePair(iter)
				sample = floatSample{t, f}
			case "histogram":
				t, fh := decodeHistogramPair(iter)
				sample = histSample{t, fh}
			default:
				iter.Skip()
			}
		}
		b.Sort()
		out = append(out, NewSeries(b.Labels(), []chunks.Sample{sample}))
	}
	return out
}

func decodeMatrix(iter *jsoniter.Iterator) []storage.Series {
	var out []storage.Series
	var b labels.ScratchBuilder
	for iter.ReadArray() {
		b.Reset()
		var samples []chunks.Sample
		hadHist := false
		for k := iter.ReadObject(); k != ""; k = iter.ReadObject() {
			switch k {
			case "metric":
				readMetric(iter, &b)
			case "values":
				for iter.ReadArray() {
					t, f := decodeSamplePair(iter)
					samples = append(samples, floatSample{t, f})
				}
			case "histograms":
				hadHist = true
				for iter.ReadArray() {
					t, fh := decodeHistogramPair(iter)
					samples = append(samples, histSample{t, fh})
				}
			default:
				iter.Skip()
			}
		}
		// A series may carry both float and histogram samples (after a type
		// transition); the storage iterator expects them in timestamp order.
		if hadHist {
			sort.SliceStable(samples, func(i, j int) bool { return samples[i].T() < samples[j].T() })
		}
		b.Sort()
		out = append(out, NewSeries(b.Labels(), samples))
	}
	return out
}

// readMetric reads a {"name":"value",...} object straight into the builder.
func readMetric(iter *jsoniter.Iterator, b *labels.ScratchBuilder) {
	for name := iter.ReadObject(); name != ""; name = iter.ReadObject() {
		b.Add(name, iter.ReadString())
	}
}

// decodeSamplePair reads a [<unix-seconds-float>, "<value>"] pair and returns a
// millisecond timestamp and float value (NaN/+Inf/-Inf handled by ParseFloat).
func decodeSamplePair(iter *jsoniter.Iterator) (int64, float64) {
	iter.ReadArray()
	ts := iter.ReadFloat64()
	iter.ReadArray()
	vs := iter.ReadString()
	iter.ReadArray() // consume closing ]
	f, err := strconv.ParseFloat(vs, 64)
	if err != nil {
		iter.ReportError("decodeSamplePair", err.Error())
	}
	return int64(ts * 1000), f
}

// decodeHistogramPair reads a [<unix-seconds-float>, {histogram}] pair. The
// histogram object is decoded into a model.SampleHistogram (its JSON shape) and
// converted to a histogram.FloatHistogram via the same path the model.Value
// iterator uses, so the result is identical.
func decodeHistogramPair(iter *jsoniter.Iterator) (int64, *histogram.FloatHistogram) {
	iter.ReadArray()
	ts := iter.ReadFloat64()
	iter.ReadArray()
	objBytes := iter.SkipAndReturnBytes()
	iter.ReadArray() // consume closing ]
	var sh model.SampleHistogram
	if err := jsonCfg.Unmarshal(objBytes, &sh); err != nil {
		iter.ReportError("decodeHistogramPair", err.Error())
		return int64(ts * 1000), nil
	}
	return int64(ts * 1000), sampleHistogramToFloatHistogram(&sh)
}

// sampleHistogramToFloatHistogram converts the API's model.SampleHistogram
// (flat bucket list) to a histogram.FloatHistogram. JSON-sourced histograms
// carry no original FloatHistogram, so this is a best-effort reconstruction as
// a custom-buckets histogram (Schema=-53); empty buckets are not preserved by
// the JSON form. (promxy's remote_read path keeps full fidelity separately.)
func sampleHistogramToFloatHistogram(sh *model.SampleHistogram) *histogram.FloatHistogram {
	if sh == nil {
		return nil
	}
	fh := &histogram.FloatHistogram{
		Schema: histogram.CustomBucketsSchema,
		Count:  float64(sh.Count),
		Sum:    float64(sh.Sum),
	}
	if len(sh.Buckets) == 0 {
		return fh
	}
	customValues := make([]float64, 0, len(sh.Buckets))
	counts := make([]float64, 0, len(sh.Buckets))
	for _, b := range sh.Buckets {
		upper := float64(b.Upper)
		if !math.IsInf(upper, 1) {
			customValues = append(customValues, upper)
		}
		counts = append(counts, float64(b.Count))
	}
	fh.CustomValues = customValues
	fh.PositiveBuckets = counts
	fh.PositiveSpans = []histogram.Span{{Offset: 0, Length: uint32(len(counts))}}
	return fh
}

// FloatSample and HistogramSample expose the package's chunks.Sample
// implementations so other packages (e.g. promxy's HA merge) can build
// storage.Series without re-deriving sample types.
func FloatSample(t int64, v float64) chunks.Sample { return floatSample{t, v} }

func HistogramSample(t int64, fh *histogram.FloatHistogram) chunks.Sample {
	return histSample{t, fh}
}
