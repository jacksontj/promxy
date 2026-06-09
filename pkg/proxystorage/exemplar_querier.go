package proxystorage

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// proxyExemplarQuerier implements storage.ExemplarQuerier on top of
// promxy's promclient.API. The upstream queryExemplars HTTP handler parses
// the `query` form param, extracts every vector selector via
// parser.ExtractSelectors, and hands the resulting matcher sets to us as
// the variadic argument to Select. We turn each matcher set back into a
// PromQL selector string and submit it to the downstream's
// /api/v1/query_exemplars endpoint, then convert the v1 response shape
// into the storage layer's exemplar.QueryResult slice.
type proxyExemplarQuerier struct {
	ctx    context.Context
	client promclient.API
}

func (q *proxyExemplarQuerier) Select(start, end int64, matchers ...[]*labels.Matcher) ([]exemplar.QueryResult, error) {
	startT := timestamp.Time(start)
	endT := timestamp.Time(end)

	// Merge per-selector results by series labels: a query can extract
	// multiple selectors that match the same series, and we'd otherwise
	// emit duplicates that grafana then has to dedup itself.
	merged := map[uint64]*exemplar.QueryResult{}
	for _, ms := range matchers {
		query, err := promhttputil.MatcherToString(ms)
		if err != nil {
			return nil, err
		}

		v, err := q.client.QueryExemplars(q.ctx, query, startT, endT)
		if err != nil {
			return nil, err
		}
		for _, r := range v {
			lbls := labelSetToLabels(r.SeriesLabels)
			fp := lbls.Hash()
			existing, ok := merged[fp]
			if !ok {
				existing = &exemplar.QueryResult{SeriesLabels: lbls}
				merged[fp] = existing
			}
			for _, ex := range r.Exemplars {
				existing.Exemplars = append(existing.Exemplars, exemplar.Exemplar{
					Labels: labelSetToLabels(ex.Labels),
					Value:  float64(ex.Value),
					Ts:     int64(ex.Timestamp),
					HasTs:  true,
				})
			}
		}
	}

	out := make([]exemplar.QueryResult, 0, len(merged))
	for _, r := range merged {
		out = append(out, *r)
	}
	return out, nil
}

func labelSetToLabels(ls model.LabelSet) labels.Labels {
	b := labels.NewScratchBuilder(len(ls))
	for k, v := range ls {
		b.Add(string(k), string(v))
	}
	b.Sort()
	return b.Labels()
}

// ExemplarQuerier returns a new ExemplarQuerier on the storage.
func (p *ProxyStorage) ExemplarQuerier(ctx context.Context) (storage.ExemplarQuerier, error) {
	return &proxyExemplarQuerier{
		ctx:    ctx,
		client: p.GetState().client,
	}, nil
}
