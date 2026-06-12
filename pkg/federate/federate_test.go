package federate

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	commonexpfmt "github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

func sp(s string) *string   { return &s }
func fp(f float64) *float64 { return &f }
func ip(i int64) *int64     { return &i }

// referenceFederate is a faithful copy of the vendored Prometheus federation
// handler (web/federate.go) for the text/float path, encoding via
// common/expfmt. It is the equivalence oracle for our handler.
func referenceFederate(q storage.Queryable, lookback time.Duration, now time.Time, extCfg labels.Labels, req *http.Request) ([]byte, error) {
	if err := req.ParseForm(); err != nil {
		return nil, err
	}
	matcherSets, err := parser.ParseMetricSelectors(req.Form["match[]"])
	if err != nil {
		return nil, err
	}

	mint := timestamp.FromTime(now.Add(-lookback))
	maxt := timestamp.FromTime(now)
	format := commonexpfmt.Negotiate(req.Header)
	var buf bytes.Buffer
	enc := commonexpfmt.NewEncoder(&buf, format)

	querier, err := q.Querier(mint, maxt)
	if err != nil {
		return nil, err
	}
	defer querier.Close()

	vec := make([]promql.Sample, 0, 8000)
	hints := &storage.SelectHints{Start: mint, End: maxt}
	var sets []storage.SeriesSet
	for _, mset := range matcherSets {
		sets = append(sets, querier.Select(context.Background(), true, hints, mset...))
	}
	set := storage.NewMergeSeriesSet(sets, 0, storage.ChainedSeriesMerge)
	it := storage.NewBuffer(int64(lookback / 1e6))
	var chkIter chunkenc.Iterator
Loop:
	for set.Next() {
		s := set.At()
		chkIter = s.Iterator(chkIter)
		it.Reset(chkIter)
		var (
			t  int64
			f  float64
			fh *histogram.FloatHistogram
		)
		switch it.Seek(maxt) {
		case chunkenc.ValFloat:
			t, f = it.At()
		case chunkenc.ValFloatHistogram, chunkenc.ValHistogram:
			t, fh = it.AtFloatHistogram(nil)
		default:
			sample, ok := it.PeekBack(1)
			if !ok {
				continue Loop
			}
			t = sample.T()
			switch sample.Type() {
			case chunkenc.ValFloat:
				f = sample.F()
			case chunkenc.ValHistogram:
				fh = sample.H().ToFloat(nil)
			case chunkenc.ValFloatHistogram:
				fh = sample.FH()
			default:
				continue Loop
			}
		}
		if value.IsStaleNaN(f) || (fh != nil && value.IsStaleNaN(fh.Sum)) {
			continue
		}
		vec = append(vec, promql.Sample{Metric: s.Labels(), T: t, F: f, H: fh})
	}
	if set.Err() != nil {
		return nil, set.Err()
	}

	slices.SortFunc(vec, func(a, b promql.Sample) int {
		return strings.Compare(a.Metric.Get(labels.MetricName), b.Metric.Get(labels.MetricName))
	})

	externalLabels := extCfg.Map()
	if _, ok := externalLabels[model.InstanceLabel]; !ok {
		externalLabels[model.InstanceLabel] = ""
	}
	externalLabelNames := make([]string, 0, len(externalLabels))
	for ln := range externalLabels {
		externalLabelNames = append(externalLabelNames, ln)
	}
	sort.Strings(externalLabelNames)

	var (
		lastMetricName string
		protMetricFam  *dto.MetricFamily
	)
	for _, s := range vec {
		if s.H != nil {
			continue // text format can't carry native histograms
		}
		nameSeen := false
		globalUsed := map[string]struct{}{}
		protMetric := &dto.Metric{Untyped: &dto.Untyped{}}
		err := s.Metric.Validate(func(l labels.Label) error {
			if l.Value == "" {
				return nil
			}
			if l.Name == labels.MetricName {
				nameSeen = true
				if l.Value == lastMetricName {
					return nil
				}
				if protMetricFam != nil {
					if err := enc.Encode(protMetricFam); err != nil {
						return err
					}
				}
				protMetricFam = &dto.MetricFamily{Type: dto.MetricType_UNTYPED.Enum(), Name: sp(l.Value)}
				lastMetricName = l.Value
				return nil
			}
			protMetric.Label = append(protMetric.Label, &dto.LabelPair{Name: sp(l.Name), Value: sp(l.Value)})
			if _, ok := externalLabels[l.Name]; ok {
				globalUsed[l.Name] = struct{}{}
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
		if !nameSeen {
			continue
		}
		for _, ln := range externalLabelNames {
			if _, ok := globalUsed[ln]; !ok {
				protMetric.Label = append(protMetric.Label, &dto.LabelPair{Name: sp(ln), Value: sp(externalLabels[ln])})
			}
		}
		protMetric.TimestampMs = ip(s.T)
		protMetric.Untyped.Value = fp(s.F)
		protMetricFam.Metric = append(protMetricFam.Metric, protMetric)
	}
	if protMetricFam != nil {
		if err := enc.Encode(protMetricFam); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

// TestFederateFallsBackForNonText asserts that a request negotiating a non-text
// format (e.g. protobuf, which can carry native histograms) is delegated to the
// fallback (vendored) handler rather than handled by the fast text path.
func TestFederateFallsBackForNonText(t *testing.T) {
	st := promqltest.LoadedStorage(t, "load 1m\n  up 1 1 1\n")
	t.Cleanup(func() { st.Close() })

	fellBack := false
	h := New(st, 5*time.Minute, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fellBack = true
		w.WriteHeader(http.StatusOK)
	}))
	h.now = func() time.Time { return timestamp.Time(120000) }

	req := httptest.NewRequest(http.MethodGet, "/federate?match%5B%5D=up", nil)
	req.Header.Set("Accept", string(commonexpfmt.NewFormat(commonexpfmt.TypeProtoDelim)))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if !fellBack {
		t.Fatal("expected protobuf request to fall back to the vendored handler")
	}
}

// TestFederateMatchesVendored asserts our handler's output is byte-for-byte
// identical to the vendored federation handler across a range of selectors and
// external-label configurations.
func TestFederateMatchesVendored(t *testing.T) {
	st := promqltest.LoadedStorage(t, `
load 1m
  http_requests_total{code="200",method="get"}   1 2 3
  http_requests_total{code="500",method="post"}   0 0 1
  http_requests_total{code="200",method="post",instance="i-1"}  7 8 9
  node_cpu{cpu="0"}   0.1 0.2 0.3
  up   1 1 1
`)
	t.Cleanup(func() { st.Close() })

	now := timestamp.Time(120000) // last loaded sample is at t=120s
	lookback := 5 * time.Minute

	extCases := map[string]labels.Labels{
		"none":              labels.EmptyLabels(),
		"source_only":       labels.FromStrings("source", "promxy"),
		"with_instance":     labels.FromStrings("source", "promxy", "instance", "fed-1"),
		"collide_with_code": labels.FromStrings("code", "EXT", "region", "us"),
	}
	matchCases := []string{
		`{__name__="http_requests_total"}`,
		`up`,
		`{__name__=~".+"}`,
		`node_cpu`,
	}

	for extName, ext := range extCases {
		for _, match := range matchCases {
			t.Run(extName+"/"+match, func(t *testing.T) {
				target := "/federate?match%5B%5D=" + url.QueryEscape(match)

				h := New(st, lookback, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
					t.Fatal("unexpected fallback to vendored handler for text/plain request")
				}))
				h.now = func() time.Time { return now }
				h.SetExternalLabels(ext)

				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

				want, err := referenceFederate(st, lookback, now, ext, httptest.NewRequest(http.MethodGet, target, nil))
				if err != nil {
					t.Fatalf("reference: %v", err)
				}

				if got := rec.Body.Bytes(); !bytes.Equal(got, want) {
					t.Fatalf("output mismatch\n got: %q\nwant: %q", got, want)
				}
			})
		}
	}
}
