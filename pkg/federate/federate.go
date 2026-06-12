// Package federate provides a fast /federate handler for promxy.
//
// It is a drop-in replacement for the vendored Prometheus federation handler
// for the common case (text/plain exposition of float samples), avoiding the
// per-request dto.MetricFamily tree and the encode-time name re-validation that
// dominate federation CPU/allocations (issue #784). For any other negotiated
// format (protobuf, OpenMetrics) -- and thus for native histograms, which are
// only servable over those formats -- it transparently delegates to a fallback
// handler (the vendored one), so behavior is unchanged outside the fast path.
//
// The query path (selectors, lookback window, merge, staleness handling, sort,
// external-label attachment) mirrors the vendored handler exactly, and the
// output is byte-for-byte identical (verified by tests against the vendored
// handler); only the encoding is replaced with pkg/expfmt.
package federate

import (
	"net/http"
	"slices"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	commonexpfmt "github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/sirupsen/logrus"

	promexpfmt "github.com/jacksontj/promxy/pkg/expfmt"
)

// Handler serves /federate. Construct with New and keep external labels current
// via SetExternalLabels on each config reload.
type Handler struct {
	queryable storage.Queryable
	lookback  time.Duration
	fallback  http.Handler

	// extLabels holds the federation external labels (sorted, including the
	// implicit empty "instance" default), swapped atomically on config reload.
	extLabels atomic.Pointer[[]labels.Label]

	now func() time.Time // overridable in tests
}

// New returns a federation handler that queries queryable over the given
// lookback window and delegates non-text formats to fallback.
func New(queryable storage.Queryable, lookback time.Duration, fallback http.Handler) *Handler {
	h := &Handler{queryable: queryable, lookback: lookback, fallback: fallback, now: time.Now}
	empty := []labels.Label{{Name: model.InstanceLabel, Value: ""}}
	h.extLabels.Store(&empty)
	return h
}

// SetExternalLabels updates the external labels attached to federated series.
// It mirrors the vendored handler: the configured global external_labels plus
// an implicit empty "instance" label when not otherwise set. Safe for
// concurrent use with ServeHTTP.
func (h *Handler) SetExternalLabels(ext labels.Labels) {
	m := map[string]string{}
	ext.Range(func(l labels.Label) { m[l.Name] = l.Value })
	if _, ok := m[model.InstanceLabel]; !ok {
		m[model.InstanceLabel] = ""
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]labels.Label, 0, len(names))
	for _, n := range names {
		out = append(out, labels.Label{Name: n, Value: m[n]})
	}
	h.extLabels.Store(&out)
}

type floatSample struct {
	metric labels.Labels
	t      int64
	f      float64
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	// Only the text/plain exposition path is handled here; protobuf/OpenMetrics
	// (and therefore native histograms) fall back to the vendored handler.
	format := commonexpfmt.Negotiate(req.Header)
	if format.FormatType() != commonexpfmt.TypeTextPlain {
		h.fallback.ServeHTTP(w, req)
		return
	}

	ctx := req.Context()
	if err := req.ParseForm(); err != nil {
		http.Error(w, "error parsing form values: "+err.Error(), http.StatusBadRequest)
		return
	}
	matcherSets, err := parser.ParseMetricSelectors(req.Form["match[]"])
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	mint := timestamp.FromTime(h.now().Add(-h.lookback))
	maxt := timestamp.FromTime(h.now())

	q, err := h.queryable.Querier(mint, maxt)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer q.Close()

	hints := &storage.SelectHints{Start: mint, End: maxt}
	var sets []storage.SeriesSet
	for _, mset := range matcherSets {
		sets = append(sets, q.Select(ctx, true, hints, mset...))
	}
	set := storage.NewMergeSeriesSet(sets, 0, storage.ChainedSeriesMerge)

	it := storage.NewBuffer(int64(h.lookback / 1e6))
	var chkIter chunkenc.Iterator
	var vec []floatSample
	for set.Next() {
		s := set.At()
		chkIter = s.Iterator(chkIter)
		it.Reset(chkIter)

		var (
			t int64
			f float64
		)
		switch it.Seek(maxt) {
		case chunkenc.ValFloat:
			t, f = it.At()
		case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
			// Native histograms are not representable in the text format; the
			// vendored handler drops them here too.
			continue
		default:
			sample, ok := it.PeekBack(1)
			if !ok || sample.Type() != chunkenc.ValFloat {
				continue
			}
			t, f = sample.T(), sample.F()
		}
		// Exposition formats have no stale marker; drop stale samples.
		if value.IsStaleNaN(f) {
			continue
		}
		vec = append(vec, floatSample{metric: s.Labels(), t: t, f: f})
	}
	if ws := set.Warnings(); len(ws) > 0 {
		logrus.WithField("warnings", ws).Debug("federation select returned warnings")
	}
	if err := set.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	slices.SortFunc(vec, func(a, b floatSample) int {
		return strings.Compare(a.metric.Get(labels.MetricName), b.metric.Get(labels.MetricName))
	})

	external := *h.extLabels.Load()
	w.Header().Set("Content-Type", string(format))
	enc := promexpfmt.NewEncoder(w, format.ToEscapingScheme())
	for i := range vec {
		s := &vec[i]
		enc.WriteFloatSample(s.metric.Get(labels.MetricName), s.metric, external, s.f, s.t)
	}
	if err := enc.Flush(); err != nil {
		logrus.WithError(err).Error("federation failed")
	}
}
