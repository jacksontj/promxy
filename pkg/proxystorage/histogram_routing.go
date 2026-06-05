package proxystorage

import (
	"context"
	"fmt"
	"sync/atomic"

	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"

	"github.com/jacksontj/promxy/pkg/promapi"
)

// histogramFinder is a parser.Visitor that flags the AST subtree as
// histogram-bearing on the first match. NodeReplacer folds it into the
// existing MultiVisitor so the histogram check costs nothing extra in
// terms of tree walks — the MultiVisitor's mutex serializes per-visitor
// calls, but we still use atomic.Bool because parser.Walk fans out child
// visits to goroutines for multi-child nodes when the finder is used
// directly (as in the isHistogramExpr unit-test wrapper).
//
// We can't reuse promclient.BooleanFinder for the same reason — its
// Found field is a plain int incremented from Visit, which races
// outside the MultiVisitor's lock.
type histogramFinder struct {
	isHistogramName func(string) bool
	found           atomic.Bool
}

func (h *histogramFinder) Visit(n parser.Node, _ []parser.Node) (parser.Visitor, error) {
	if h.found.Load() {
		return h, nil
	}
	switch x := n.(type) {
	case *parser.Call:
		if _, ok := histogramOnlyFuncs[x.Func.Name]; ok {
			h.found.Store(true)
		}
	case *parser.VectorSelector:
		if h.isHistogramName != nil && h.isHistogramName(x.Name) {
			h.found.Store(true)
		}
	}
	return h, nil
}

// pathHasHistogramOnlyCall reports whether path contains a Call to one
// of the histogram-only PromQL functions above. Used so descendants of
// an already-flagged histogram-only call inherit the signal — without
// this, parser.Walk descending into a node we returned nil for would
// re-visit the inner selector with no context.
func pathHasHistogramOnlyCall(path []parser.Node) bool {
	for _, p := range path {
		if c, ok := p.(*parser.Call); ok {
			if _, hist := histogramOnlyFuncs[c.Func.Name]; hist {
				return true
			}
		}
	}
	return false
}

// histogramOnlyFuncs are the PromQL functions that operate exclusively on
// native histograms — the engine errors at evaluation time if their input
// isn't a histogram. A subtree containing a Call to one of these is
// unambiguously histogram-bearing, no metadata required.
//
// histogram_quantile is intentionally NOT in this set: it accepts both
// classic _bucket float series AND native histograms, so it doesn't on
// its own signal a histogram-bearing query.
var histogramOnlyFuncs = map[string]struct{}{
	"histogram_avg":      {},
	"histogram_count":    {},
	"histogram_sum":      {},
	"histogram_stddev":   {},
	"histogram_stdvar":   {},
	"histogram_fraction": {},
}

// isHistogramExpr reports whether node — taken together with the path of
// ancestors leading to it — is part of a query that produces or consumes
// native-histogram samples. Two signals flag it:
//
//  1. A Call to one of the histogram-only PromQL functions above appears
//     in either the path (ancestors) OR the subtree rooted at node.
//     Ancestors matter because parser.Walk descends into a node's children
//     even when NodeReplacer returns nil for the node itself; without the
//     ancestor check, the bare VectorSelector inside histogram_count(foo)
//     would look like a plain float selector when NodeReplacer visits it.
//  2. A VectorSelector whose metric name is reported histogram-typed by
//     isHistogramName. isHistogramName may be nil, in which case the
//     metric-name signal is skipped — that's the AST-only mode used when
//     no server group has histogram_metadata_refresh configured.
//
// Ambiguous nodes (histogram_quantile, rate() on an unknown metric, etc.)
// don't flag the expression. NodeReplacer uses this to opt out of HTTP-API
// pushdown for histogram queries so the embedded engine evaluates locally
// and fetches raw data via remote_read (where the sparse-span schema is
// preserved end-to-end).
func isHistogramExpr(ctx context.Context, node parser.Node, path []parser.Node, isHistogramName func(string) bool) bool {
	if pathHasHistogramOnlyCall(path) {
		return true
	}
	f := &histogramFinder{isHistogramName: isHistogramName}
	_, _ = parser.Walk(ctx, f, nil, node, nil, nil)
	return f.found.Load()
}

// strictMissingRemoteRead returns the ordinals of server groups that have
// neither remote_read configured nor native_histogram.allow_lossy enabled —
// i.e. groups that can't serve a histogram-bearing query without silently
// degrading. Used by NodeReplacer to fail loud when a histogram query
// lands on a server group whose operator hasn't opted into lossiness.
func (p *ProxyStorage) strictMissingRemoteRead() []int {
	state := p.GetState()
	if state == nil {
		return nil
	}
	var ords []int
	for _, sg := range state.sgs {
		if sg.Cfg == nil {
			continue
		}
		if !sg.Cfg.RemoteRead && !sg.Cfg.NativeHistogram.AllowLossy {
			ords = append(ords, sg.Cfg.Ordinal)
		}
	}
	return ords
}

// histogramNamePredicate returns a predicate over metric names that reports
// true when ANY server group's metadata cache lists the name as a
// histogram. The OR semantic is intentional: if one upstream serves a
// metric as a histogram and another as a float, we must route the query
// as histogram-bearing or we lose fidelity on the histogram side.
//
// Returns nil when no server group has the cache enabled — callers can
// use that to skip the metric-name leg of the AST walk entirely.
func (p *ProxyStorage) histogramNamePredicate() func(string) bool {
	state := p.GetState()
	if state == nil {
		return nil
	}
	var enabled bool
	for _, sg := range state.sgs {
		if sg.Cfg != nil && sg.Cfg.NativeHistogram.MetadataRefresh > 0 {
			enabled = true
			break
		}
	}
	if !enabled {
		return nil
	}
	sgs := state.sgs
	return func(name string) bool {
		for _, sg := range sgs {
			if sg.IsHistogramMetric(name) {
				return true
			}
		}
		return false
	}
}

// histogramFidelityError formats the strict-mode error returned when a
// histogram-bearing query targets server groups without remote_read
// configured.
func histogramFidelityError(missing []int) error {
	return fmt.Errorf("native histogram query targets server_group(s) ordinal=%v without remote_read; configure remote_read on those groups (preferred — preserves sparse-span schema) or set native_histogram.allow_lossy: true to accept JSON-API fidelity loss", missing)
}

// containsLossyHistogram drains ss into an in-memory, re-iterable SeriesSet
// (copying every sample) and reports whether any series carries a native-
// histogram sample. We treat ANY histogram in an HTTP-pushdown response as
// lossy because the JSON SampleHistogram shape can't preserve sparse-span
// schema, empty buckets, or the trailing CustomValues boundary that's
// implied by the next bucket. Detecting only the obvious patterns
// (zero-bucket, non-contiguous) still mis-handles single-bucket NHCB
// and any exponential-schema histogram.
//
// The set is materialized — not merely peeked — because a storage.SeriesSet
// is a one-shot cursor: detecting histograms consumes it, yet the same data
// must still flow to UnexpandedSeriesSet for the engine to read. The
// returned set is a fresh copy callers assign back over result. This matches
// the pre-refactor behavior where the promclient API returned a fully-decoded
// model.Value. (The pushdown path always resolves through the HTTP client —
// PromAPIRemoteRead only overrides GetValue — so this never drains the lazy
// remote_read stream.)
//
// Post-hoc safety net for queries the AST walker can't pre-flag. When a
// histogram is found, NodeReplacer aborts pushdown and lets the engine
// evaluate locally — Select → GetValue routes through remote_read where the
// original FloatHistogram is preserved end-to-end.
//
// Trade-off: histogram-bearing queries that the AST didn't flag pay
// one extra HTTP round-trip before the fallback fires. Pure-float
// queries are unaffected. To avoid the extra trip, enable the
// metric-name cache (native_histogram.metadata_refresh) so the AST
// walker catches histogram metrics up front.
func containsLossyHistogram(ss storage.SeriesSet) (storage.SeriesSet, bool) {
	var (
		series  []storage.Series
		hasHist bool
	)
	for ss.Next() {
		s := ss.At()
		lbls := s.Labels().Copy()
		var samples []chunks.Sample
		it := s.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				t, v := it.At()
				samples = append(samples, promapi.FloatSample(t, v))
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				hasHist = true
				t, fh := it.AtFloatHistogram(nil)
				samples = append(samples, promapi.HistogramSample(t, fh))
			}
		}
		if err := it.Err(); err != nil {
			return promapi.NewSeriesSet(nil, ss.Warnings(), err), hasHist
		}
		series = append(series, promapi.NewSeries(lbls, samples))
	}
	return promapi.NewSeriesSet(series, ss.Warnings(), ss.Err()), hasHist
}
