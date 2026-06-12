package promclient

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promhttputil"
)

func metricToLbls(m model.Metric) labels.Labels {
	b := labels.NewScratchBuilder(len(m))
	for k, v := range m {
		b.Add(string(k), string(v))
	}
	b.Sort()
	return b.Labels()
}

func matrixToSeriesSet(mx model.Matrix) storage.SeriesSet {
	var series []storage.Series
	for _, ss := range mx {
		var samples []chunks.Sample
		for _, p := range ss.Values {
			samples = append(samples, promapi.FloatSample(int64(p.Timestamp), float64(p.Value)))
		}
		series = append(series, storage.NewListSeries(metricToLbls(ss.Metric), samples))
	}
	return promapi.NewSeriesSet(series, nil, nil)
}

func dumpSS(t *testing.T, ss storage.SeriesSet) map[string]string {
	t.Helper()
	out := map[string]string{}
	for ss.Next() {
		s := ss.At()
		var b strings.Builder
		it := s.Iterator(nil)
		for it.Next() != chunkenc.ValNone {
			ts, v := it.At()
			fmt.Fprintf(&b, "%d=%s ", ts, strconv.FormatFloat(v, 'g', -1, 64))
		}
		out[s.Labels().String()] = b.String()
	}
	return out
}

func dumpMatrix(mx model.Matrix) map[string]string {
	out := map[string]string{}
	for _, ss := range mx {
		var b strings.Builder
		ps := append([]model.SamplePair(nil), ss.Values...)
		sort.Slice(ps, func(i, j int) bool { return ps[i].Timestamp < ps[j].Timestamp })
		for _, p := range ps {
			fmt.Fprintf(&b, "%d=%s ", int64(p.Timestamp), strconv.FormatFloat(float64(p.Value), 'g', -1, 64))
		}
		out[metricToLbls(ss.Metric).String()] = b.String()
	}
	return out
}

func stream(name string, pts ...float64) *model.SampleStream {
	ss := &model.SampleStream{Metric: model.Metric{"__name__": model.LabelValue(name)}}
	for i := 0; i+1 < len(pts); i += 2 {
		ss.Values = append(ss.Values, model.SamplePair{Timestamp: model.Time(pts[i]), Value: model.SampleValue(pts[i+1])})
	}
	return ss
}

// TestMergeSeriesSetsMatchesMergeValues asserts the SeriesSet adapter produces
// the same merged result as the existing model.Value MergeValues, across the
// anti-affinity behaviours (overlap, holes, base-by-point-count, preferMax,
// disjoint series).
func TestMergeSeriesSetsMatchesMergeValues(t *testing.T) {
	cases := []struct {
		name         string
		antiAffinity model.Time
		preferMax    bool
		a, b         model.Matrix
	}{
		{
			name:         "identical_overlap",
			antiAffinity: 2,
			a:            model.Matrix{stream("m", 0, 1, 10, 2, 20, 3)},
			b:            model.Matrix{stream("m", 0, 1, 10, 2, 20, 3)},
		},
		{
			name:         "b_has_more_points_fills_hole",
			antiAffinity: 2,
			a:            model.Matrix{stream("m", 0, 1, 20, 3)},
			b:            model.Matrix{stream("m", 0, 1, 10, 2, 20, 3, 30, 4)},
		},
		{
			name:         "prefer_max_on_overlap",
			antiAffinity: 2,
			preferMax:    true,
			a:            model.Matrix{stream("m", 0, 5, 10, 1)},
			b:            model.Matrix{stream("m", 0, 2, 10, 9)},
		},
		{
			name:         "disjoint_series",
			antiAffinity: 2,
			a:            model.Matrix{stream("a", 0, 1)},
			b:            model.Matrix{stream("b", 0, 2)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			want, err := promhttputil.MergeValues(tc.antiAffinity, tc.a, tc.b, tc.preferMax)
			if err != nil {
				t.Fatalf("MergeValues: %v", err)
			}
			wantDump := dumpMatrix(want.(model.Matrix))

			got := MergeSeriesSets(tc.antiAffinity, tc.preferMax, matrixToSeriesSet(tc.a), matrixToSeriesSet(tc.b))
			gotDump := dumpSS(t, got)

			if len(gotDump) != len(wantDump) {
				t.Fatalf("series count: got %d want %d\n got=%v\nwant=%v", len(gotDump), len(wantDump), gotDump, wantDump)
			}
			for k, w := range wantDump {
				if gotDump[k] != w {
					t.Fatalf("series %s: got %q want %q", k, gotDump[k], w)
				}
			}
		})
	}
}

// TestMergeSeriesSetsEdgeCases covers the non-merge paths: no sets, and the
// single-set fast path (which returns the set directly and must therefore
// preserve its warnings and data unchanged).
func TestMergeSeriesSetsEdgeCases(t *testing.T) {
	t.Run("no_sets", func(t *testing.T) {
		ss := MergeSeriesSets(0, false)
		if ss.Next() {
			t.Fatal("expected empty result")
		}
		if ss.Err() != nil {
			t.Fatalf("expected no error, got %v", ss.Err())
		}
	})

	t.Run("single_set_preserves_warnings_and_data", func(t *testing.T) {
		warn := annotations.New().Add(errors.New("a warning"))
		single := matrixToSeriesSet(model.Matrix{stream("m", 0, 1, 10, 2)})
		single = WithWarnings(single, warn)

		got := MergeSeriesSets(0, false, single)
		dump := dumpSS(t, got)
		if len(dump) != 1 {
			t.Fatalf("expected 1 series, got %d: %v", len(dump), dump)
		}
		if len(got.Warnings().AsErrors()) != 1 {
			t.Fatalf("expected warnings preserved through single-set path, got %v", got.Warnings().AsErrors())
		}
	})
}

// flakySeries reads its samples lazily and, once its source is "canceled"
// (mimicking the remote_read HTTP body being closed / context canceled after
// GetValue returns), yields nothing. It models the lazy ChunkedSeriesSet that
// materializeSeriesSet must drain eagerly.
type flakySeries struct {
	lbls     labels.Labels
	samples  []chunks.Sample
	canceled *bool
}

func (s *flakySeries) Labels() labels.Labels { return s.lbls }
func (s *flakySeries) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	if *s.canceled {
		return storage.NewListSeries(s.lbls, nil).Iterator(nil)
	}
	return storage.NewListSeries(s.lbls, s.samples).Iterator(nil)
}

func newFlakySeriesSet(series ...storage.Series) storage.SeriesSet {
	return promapi.NewSeriesSet(series, nil, nil)
}

// TestMaterializeSeriesSetDecouplesFromSource asserts materializeSeriesSet drains
// the source eagerly, so the returned SeriesSet still yields every sample after
// the source goes dead -- the exact guarantee the remote_read path depends on.
func TestMaterializeSeriesSetDecouplesFromSource(t *testing.T) {
	canceled := false
	samples := []chunks.Sample{promapi.FloatSample(0, 1), promapi.FloatSample(10, 2)}
	src := newFlakySeriesSet(&flakySeries{
		lbls:     labels.FromStrings("__name__", "m"),
		samples:  samples,
		canceled: &canceled,
	})

	materialized := materializeSeriesSet(src)

	// Source goes dead, as if GetValue had returned and the body/context closed.
	canceled = true

	got := dumpSS(t, materialized)
	want := map[string]string{`{__name__="m"}`: "0=1 10=2 "}
	if len(got) != 1 || got[`{__name__="m"}`] != want[`{__name__="m"}`] {
		t.Fatalf("materialized result lost data after source cancel: got %v want %v", got, want)
	}

	// Sanity: a fresh flaky source that is canceled before reading yields nothing,
	// proving the cancel flag actually has teeth.
	canceled2 := true
	dead := newFlakySeriesSet(&flakySeries{
		lbls:     labels.FromStrings("__name__", "m"),
		samples:  samples,
		canceled: &canceled2,
	})
	if d := dumpSS(t, dead); d[`{__name__="m"}`] != "" {
		t.Fatalf("expected canceled source to yield no samples, got %v", d)
	}
}
