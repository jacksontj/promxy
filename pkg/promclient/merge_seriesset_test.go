package promclient

import (
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
