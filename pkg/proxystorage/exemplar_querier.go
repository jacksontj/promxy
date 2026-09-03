package proxystorage

import (
	"context"
	"sort"

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
	// Per-series set of exemplars already collected. The same exemplar can be
	// returned for more than one of the extracted selectors, and an exemplar is
	// only a duplicate when its timestamp, value AND labels all match -- a
	// series may legitimately have several exemplars at one timestamp with
	// different trace IDs.
	seen := map[uint64]map[exemplarKey]struct{}{}
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
				seen[fp] = make(map[exemplarKey]struct{}, len(r.Exemplars))
			}
			seenSeries := seen[fp]
			for _, ex := range r.Exemplars {
				e := exemplar.Exemplar{
					Labels: labelSetToLabels(ex.Labels),
					Value:  float64(ex.Value),
					Ts:     int64(ex.Timestamp),
					HasTs:  true,
				}
				// labels.Labels are sorted, so String() is a stable key.
				k := exemplarKey{ts: e.Ts, value: e.Value, labels: e.Labels.String()}
				if _, dup := seenSeries[k]; dup {
					continue
				}
				seenSeries[k] = struct{}{}
				existing.Exemplars = append(existing.Exemplars, e)
			}
		}
	}

	out := make([]exemplar.QueryResult, 0, len(merged))
	for _, r := range merged {
		// The downstream API returns exemplars ordered by timestamp; restore
		// that ordering after merging across selectors. The comparison is a
		// total order over exactly the fields exemplarKey identifies an
		// exemplar by, so the result doesn't depend on the (unordered) order
		// the merged-in results arrived in.
		exemplars := r.Exemplars
		sort.Slice(exemplars, func(i, j int) bool {
			a, b := exemplars[i], exemplars[j]
			if a.Ts != b.Ts {
				return a.Ts < b.Ts
			}
			if a.Value != b.Value {
				return a.Value < b.Value
			}
			return a.Labels.String() < b.Labels.String()
		})
		out = append(out, *r)
	}
	// Map iteration order is random, so sort by series labels to make repeated
	// identical requests return identical results.
	sort.Slice(out, func(i, j int) bool { return labels.Compare(out[i].SeriesLabels, out[j].SeriesLabels) < 0 })
	return out, nil
}

// exemplarKey is the identity of a single exemplar within a series: timestamp,
// value and labels together.
type exemplarKey struct {
	ts     int64
	value  float64
	labels string
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
