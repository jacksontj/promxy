package proxyquerier

import (
	"context"
	"sort"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// ProxyChunkQuerier implements prometheus' storage.ChunkQuerier on top of
// ProxyQuerier. Promxy has no native chunk source -- it fetches raw samples
// from the downstream server groups -- so Select runs the normal sample-based
// ProxyQuerier.Select and re-encodes the resulting series into chunks via
// storage.NewSeriesSetToChunkSet.
//
// This is what backs promxy's own remote_read endpoint (/api/v1/read) for the
// STREAMED_XOR_CHUNKS response type, which is what a stock Prometheus
// remote_read client negotiates by default. Without it that path returned a
// 500 ("not implemented"). The label-querier methods (LabelValues, LabelNames,
// Close) are promoted from the embedded *ProxyQuerier.
type ProxyChunkQuerier struct {
	*ProxyQuerier
}

// Select fetches the matching series as samples and re-encodes them as chunks.
// The remote-read streaming API requires series to be sorted by label set, so
// the result is always sorted here (ProxyQuerier.Select does not guarantee an
// order); the sortSeries hint is therefore honored unconditionally.
func (h *ProxyChunkQuerier) Select(ctx context.Context, sortSeries bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.ChunkSeriesSet {
	ss := h.ProxyQuerier.Select(ctx, sortSeries, hints, matchers...)

	// ProxyQuerier already materializes the full result, so collecting and
	// sorting here adds no extra round-trips.
	var series []storage.Series
	for ss.Next() {
		series = append(series, ss.At())
	}
	sort.Slice(series, func(i, j int) bool {
		return labels.Compare(series[i].Labels(), series[j].Labels()) < 0
	})

	// ss.Err()/ss.Warnings() are only final once iteration is drained above.
	return storage.NewSeriesSetToChunkSet(NewSeriesSet(series, ss.Warnings(), ss.Err()))
}
