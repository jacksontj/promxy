package proxystorage

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"os"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/histogram"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/model/value"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/scrape"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/agent"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
	"github.com/prometheus/prometheus/tsdb/chunks"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/logging"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promapi"
	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/proxyquerier"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// noopScrapeManager satisfies remote.ReadyScrapeManager for promxy, which has
// no local scrape manager. Returning an error here causes upstream's
// remote_write to skip metadata sending, which is the behavior we want.
type noopScrapeManager struct{}

func (noopScrapeManager) Get() (*scrape.Manager, error) {
	return nil, errors.New("promxy has no scrape manager")
}

// metricNameWorkaroundLabel is a workaround from https://github.com/jacksontj/promxy/issues/274
const metricNameWorkaroundLabel = "__name"

type proxyStorageState struct {
	sgs           []*servergroup.ServerGroup
	client        promclient.API
	cfg           *proxyconfig.Config
	remoteStorage *remote.Storage
	// agentDB writes the remote_write WAL that remoteStorage's queue managers
	// tail. It is nil when no remote_write endpoint is configured.
	agentDB *agent.DB
	// appendable hands out appenders for the rule manager. Backed by agentDB
	// when remote_write is configured, otherwise by a no-op stub.
	appendable     storage.Appendable
	appenderCloser func() error
}

// Ready blocks until all servergroups are ready
func (p *proxyStorageState) Ready() {
	for i, sg := range p.sgs {
		select {
		case <-time.After(time.Second * 5):
			logrus.Debugf("Servergroup %d taking a long time to be Ready (still waiting)", i)
			<-sg.Ready
		case <-sg.Ready:
			continue
		}

	}
}

// Cancel this state
func (p *proxyStorageState) Cancel(n *proxyStorageState) {
	if p.sgs != nil {
		for _, sg := range p.sgs {
			sg.Cancel()
		}
	}
	// Close the remote_write storage (agent WAL + queue managers) unless the
	// new state is reusing the same instance.
	if p.appenderCloser != nil && (n == nil || p.remoteStorage != n.remoteStorage) {
		p.appenderCloser()
	}
}

// NewProxyStorage creates a new ProxyStorage. If localStoragePath is
// non-empty, it is used as the base directory for the remote_write WAL
// (durable across restarts); otherwise a temporary directory is created
// per remote_write configuration and removed on shutdown.
func NewProxyStorage(NoStepSubqueryIntervalFn func(rangeMillis int64) int64, localStoragePath string) (*ProxyStorage, error) {
	return &ProxyStorage{
		NoStepSubqueryIntervalFn: NoStepSubqueryIntervalFn,
		localStoragePath:         localStoragePath,
	}, nil
}

// ProxyStorage implements prometheus' Storage interface
type ProxyStorage struct {
	NoStepSubqueryIntervalFn func(rangeMillis int64) int64
	localStoragePath         string
	state                    atomic.Value
}

// GetState returns the current state of the ProxyStorage
func (p *ProxyStorage) GetState() *proxyStorageState {
	tmp := p.state.Load()
	if sg, ok := tmp.(*proxyStorageState); ok {
		return sg
	}
	return &proxyStorageState{}
}

// ApplyConfig updates the current state of this ProxyStorage
func (p *ProxyStorage) ApplyConfig(c *proxyconfig.Config) error {
	oldState := p.GetState() // Fetch the old state

	failed := false

	apis := make([]promclient.API, len(c.ServerGroups))
	newState := &proxyStorageState{
		sgs: make([]*servergroup.ServerGroup, len(c.ServerGroups)),
		cfg: c,
	}
	for i, sgCfg := range c.ServerGroups {
		sgCfg.Ordinal = i
		tmp, err := servergroup.NewServerGroup()
		if err != nil {
			failed = true
			logrus.Errorf("Error creating server group (%d): %s", i, err)
			// We can continue to other as we don't need to cancel this as we didn't ApplyConfig()
			continue
		}
		// Once the ServerGroup was created; we want to add this to the lists as there
		// are background goroutines launched (so we'd need to cancel)
		newState.sgs[i] = tmp
		apis[i] = tmp

		if err := tmp.ApplyConfig(sgCfg); err != nil {
			failed = true
			logrus.Errorf("Error applying config to server group (%d): %s", i, err)
		}
	}

	// If there was a failure anywhere, we need to cancel the newState and return an error
	if failed {
		newState.Cancel(nil)
		return fmt.Errorf("error applying config to one or more server group(s)")
	}

	multiApi, err := promclient.NewMultiAPI(apis, model.TimeFromUnix(0), false, nil, len(apis), false)
	if err != nil {
		return err
	}

	newState.client = promclient.NewTimeTruncate(multiApi)

	// Check for remote_write (for appender)
	if c.PromConfig.RemoteWriteConfigs != nil {
		if oldState.remoteStorage != nil {
			if err := oldState.remoteStorage.ApplyConfig(&c.PromConfig); err != nil {
				return err
			}
			newState.remoteStorage = oldState.remoteStorage
			newState.agentDB = oldState.agentDB
			newState.appendable = oldState.appendable
			newState.appenderCloser = oldState.appenderCloser
		} else {
			walDir := p.localStoragePath
			ephemeral := walDir == ""
			if ephemeral {
				dir, err := os.MkdirTemp("", "promxy-remote-wal-")
				if err != nil {
					return fmt.Errorf("creating remote_write WAL dir: %w", err)
				}
				walDir = dir
			}
			rwLogger := logging.NewLogger(logrus.WithField("component", "remote_write").Logger)
			rs := remote.NewStorage(
				rwLogger,
				prometheus.DefaultRegisterer,
				func() (int64, error) { return 0, nil },
				walDir,
				1*time.Second,
				noopScrapeManager{},
			)
			// promxy has no local TSDB writing a WAL, so remote.Storage's queue
			// managers have nothing to tail. Run an agent-mode WAL-only DB whose
			// appender writes the WAL that those queue managers consume. Without
			// this the WAL watcher fails with "error tailing WAL ... no such file
			// or directory" and no samples are ever shipped (see issue #771).
			db, err := agent.Open(rwLogger, prometheus.DefaultRegisterer, rs, walDir, agent.DefaultOptions())
			if err != nil {
				if ephemeral {
					os.RemoveAll(walDir)
				}
				return fmt.Errorf("creating remote_write WAL: %w", err)
			}
			// Wake the queue managers' WAL watchers as soon as samples are committed.
			db.SetWriteNotified(rs)
			if err := rs.ApplyConfig(&c.PromConfig); err != nil {
				db.Close()
				if ephemeral {
					os.RemoveAll(walDir)
				}
				return err
			}
			newState.remoteStorage = rs
			newState.agentDB = db
			newState.appendable = db
			newState.appenderCloser = func() error {
				dbErr := db.Close()
				rsErr := rs.Close()
				if ephemeral {
					os.RemoveAll(walDir)
				}
				if dbErr != nil {
					return dbErr
				}
				return rsErr
			}
		}
	} else {
		newState.appendable = appendableStub{}
	}

	newState.Ready()        // Wait for the newstate to be ready
	p.state.Store(newState) // Store the new state
	if oldState != nil {
		oldState.Cancel(newState) // Cancel the old one
	}

	return nil
}

// ConfigHandler is an implementation of the config handler within the prometheus API
func (p *ProxyStorage) ConfigHandler(w http.ResponseWriter, r *http.Request) {
	state := p.GetState()
	v := map[string]interface{}{
		"status": "success",
		"data": map[string]string{
			"yaml": state.cfg.String(),
		},
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	b, err := json.Marshal(v)
	if err != nil {
		logrus.Error("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		logrus.Error("msg", "error writing response", "bytesWritten", n, "err", err)
	}
}

// MetadataHandler is an implementation of the metadata handler within the prometheus API
func (p *ProxyStorage) MetadataHandler(w http.ResponseWriter, r *http.Request) {
	// Check that "limit" is valid
	var limit *int
	if s := r.FormValue("limit"); s != "" {
		i, err := strconv.Atoi(s)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		limit = &i
	}

	// Do the metadata lookup
	state := p.GetState()
	metadata, err := state.client.Metadata(r.Context(), r.FormValue("metric"), r.FormValue("limit"))

	// Trim the results to the requested limit
	if limit != nil && len(metadata) > *limit {
		count := 0
		for k := range metadata {
			if count < *limit {
				count++
			} else {
				delete(metadata, k)
			}
		}
	}

	var v map[string]interface{}
	if err != nil {
		v = map[string]interface{}{
			"status": "error",
			"error":  err.Error(),
		}
	} else {
		v = map[string]interface{}{
			"status": "success",
			"data":   metadata,
		}
	}

	json := jsoniter.ConfigCompatibleWithStandardLibrary
	b, err := json.Marshal(v)
	if err != nil {
		logrus.Error("msg", "error marshaling json response", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if n, err := w.Write(b); err != nil {
		logrus.Error("msg", "error writing response", "bytesWritten", n, "err", err)
	}

}

// Querier returns a new Querier on the storage.
func (p *ProxyStorage) Querier(mint, maxt int64) (storage.Querier, error) {
	state := p.GetState()
	return &proxyquerier.ProxyQuerier{
		Start:  timestamp.Time(mint).UTC(),
		End:    timestamp.Time(maxt).UTC(),
		Client: state.client,

		Cfg: &state.cfg.PromxyConfig,
	}, nil
}

// StartTime returns the oldest timestamp stored in the storage.
func (p *ProxyStorage) StartTime() (int64, error) {
	return 0, nil
}

// Appender returns a new appender against the storage. When remote_write is
// configured this is backed by the agent WAL (single-use, pooled appender), so
// a fresh appender is returned on every call rather than a shared instance.
func (p *ProxyStorage) Appender(ctx context.Context) storage.Appender {
	return p.GetState().appendable.Appender(ctx)
}

// Close releases the resources of the Querier.
func (p *ProxyStorage) Close() error { return nil }

// ChunkQuerier returns a new ChunkQuerier on the storage.
func (p *ProxyStorage) ChunkQuerier(mint, maxt int64) (storage.ChunkQuerier, error) {
	return nil, errors.New("not implemented")
}

// Implement web.LocalStorage
func (p *ProxyStorage) CleanTombstones() (err error) { return nil }
func (p *ProxyStorage) Delete(_ context.Context, mint, maxt int64, ms ...*labels.Matcher) error {
	return nil
}
func (p *ProxyStorage) Snapshot(dir string, withHead bool) error { return nil }
func (p *ProxyStorage) Stats(statsByLabelName string, _ int) (*tsdb.Stats, error) {
	return &tsdb.Stats{IndexPostingStats: &index.PostingsStats{}}, nil
}
func (p *ProxyStorage) WALReplayStatus() (tsdb.WALReplayStatus, error) {
	return tsdb.WALReplayStatus{}, errors.New("not implemented")
}

// vectorToStepMatrix converts an instant-query SeriesSet (one sample per
// series at the @ time) into a range-query-shaped SeriesSet: each input
// series' single sample is replicated at every step time in [start, end]
// (inclusive of start, inclusive of end when (end-start) is a multiple of
// step). Histograms are replicated the same way. This is only valid when the
// underlying expression is step-invariant — currently used by the NodeReplacer
// when the subtree below has an @ modifier.
//
// The replicated FloatHistogram pointer is shared across steps; that is safe
// because promapi.NewSeries hands out copy-on-read iterators, so the engine
// can't observe an aliased histogram.
func vectorToStepMatrix(vec storage.SeriesSet, start, end time.Time, step time.Duration) storage.SeriesSet {
	if step <= 0 {
		return promapi.NewSeriesSet(nil, vec.Warnings(), vec.Err())
	}
	startMs := timestamp.FromTime(start)
	endMs := timestamp.FromTime(end)
	stepMs := int64(step / time.Millisecond)
	if stepMs <= 0 || endMs < startMs {
		return promapi.NewSeriesSet(nil, vec.Warnings(), vec.Err())
	}
	n := int((endMs-startMs)/stepMs) + 1
	var out []storage.Series
	for vec.Next() {
		src := vec.At()
		lbls := src.Labels().Copy()

		// Instant query → at most one sample per series. Read it (the last
		// one wins if, defensively, more than one is present).
		var (
			haveFloat bool
			fval      float64
			fh        *histogram.FloatHistogram
		)
		it := src.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				_, fval = it.At()
				haveFloat, fh = true, nil
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				_, fh = it.AtFloatHistogram(nil)
				haveFloat = false
			}
		}
		if err := it.Err(); err != nil {
			return promapi.NewSeriesSet(nil, vec.Warnings(), err)
		}

		samples := make([]chunks.Sample, 0, n)
		for i := 0; i < n; i++ {
			ts := startMs + int64(i)*stepMs
			switch {
			case fh != nil:
				samples = append(samples, promapi.HistogramSample(ts, fh))
			case haveFloat:
				samples = append(samples, promapi.FloatSample(ts, fval))
			}
		}
		out = append(out, promapi.NewSeries(lbls, samples))
	}
	return promapi.NewSeriesSet(out, vec.Warnings(), vec.Err())
}

// NodeReplacer replaces promql Nodes with more efficient-to-fetch ones. This works by taking lower-layer
// chunks of the query, farming them out to prometheus hosts, then stitching the results back together.
// An example would be a sum, we can sum multiple sums and come up with the same result -- so we do.
// There are a few ground rules for this:
//   - Children cannot be AggregateExpr: aggregates have their own combining logic, so its not safe to send a subquery with additional aggregations
//   - offsets within the subtree must match: if they don't then we'll get mismatched data, so we wait until we are far enough down the tree that they converge
//   - Don't reduce accuracy/granularity: the intention of this is to get the correct data faster, meaning correctness overrules speed.
func (p *ProxyStorage) NodeReplacer(ctx context.Context, s *parser.EvalStmt, node parser.Node, path []parser.Node) (parser.Node, error) {
	isAgg := func(node parser.Node) bool {
		_, ok := node.(*parser.AggregateExpr)
		return ok
	}

	isSubQuery := func(node parser.Node) bool {
		_, ok := node.(*parser.SubqueryExpr)
		return ok
	}

	isBinaryExpr := func(node parser.Node) bool {
		_, ok := node.(*parser.BinaryExpr)
		return ok
	}

	isVectorSelector := func(node parser.Node) bool {
		_, ok := node.(*parser.VectorSelector)
		return ok
	}

	hasTimestamp := func(node parser.Node) bool {
		if vs, ok := node.(*parser.VectorSelector); ok {
			return vs.Timestamp != nil
		}
		return false
	}

	// isAtModifierUnsafeCall flags Call nodes whose result is NOT
	// step-invariant even when an inner @ modifier pins the input — e.g.
	// timestamp() returns the evaluation-time timestamp, predict_linear
	// extrapolates from evalTime, etc. Their presence in the subtree
	// disqualifies the instant-query optimization in queryRangeAt below.
	isAtModifierUnsafeCall := func(node parser.Node) bool {
		c, ok := node.(*parser.Call)
		if !ok {
			return false
		}
		_, unsafe := promql.AtModifierUnsafeFunctions[c.Func.Name]
		return unsafe
	}

	// If we are a child of a subquery; we just skip replacement (since it already did a nodereplacer for those)
	for _, n := range path {
		if isSubQuery(n) {
			return nil, nil
		}
	}

	// If there is a child that is an aggregator we cannot do anything (as they have their own
	// rules around combining). We'll skip this node and let a lower layer take this on
	aggFinder := &promclient.BooleanFinder{Func: isAgg}
	offsetFinder := &promclient.OffsetFinder{}
	vecFinder := &promclient.BooleanFinder{Func: isVectorSelector}
	timestampFinder := &promclient.BooleanFinder{Func: hasTimestamp}
	// atTimestampFinder records the @ timestamp itself (in ms) so we can
	// issue an instant query at a guaranteed-safe time when pushing down
	// step-invariant subtrees — see queryRangeAt.
	atTimestampFinder := &promclient.TimestampFinder{}
	// atUnsafeFinder counts Call nodes whose result depends on the
	// evaluation timestamp regardless of any inner @; if any are present
	// queryRangeAt cannot use the instant-query optimization.
	atUnsafeFinder := &promclient.BooleanFinder{Func: isAtModifierUnsafeCall}
	// histFinder rides along on the same tree walk to detect histogram-
	// bearing subtrees: histogram-only function calls (always) plus
	// VectorSelectors whose metric name is histogram-typed per the
	// per-server-group metadata cache (when native_histogram.metadata_refresh
	// is configured).
	histFinder := &histogramFinder{isHistogramName: p.histogramNamePredicate()}

	visitor := promclient.NewMultiVisitor([]parser.Visitor{aggFinder, offsetFinder, vecFinder, timestampFinder, atTimestampFinder, atUnsafeFinder, histFinder})

	if _, err := parser.Walk(ctx, visitor, s, node, nil, nil); err != nil {
		return nil, err
	}

	// Histogram-bearing queries lose schema fidelity over the HTTP API
	// (the JSON SampleHistogram shape collapses sparse spans into a flat
	// bucket list, drops empty buckets, etc.). Two-step handling:
	//   1. Fail loud if any targeted server group has neither remote_read
	//      nor native_histogram.allow_lossy — wrong data is worse than no
	//      data, so the default is to surface the misconfig.
	//   2. Otherwise opt out of pushdown so the embedded engine evaluates
	//      locally and fetches raw data through GetValue, which routes
	//      via remote_read where configured and preserves the original
	//      FloatHistogram end-to-end.
	// Ancestor-path inheritance: parser.Walk descends into children even
	// when NodeReplacer returns nil for the parent, so a histogram-only
	// call above us still propagates the histogram signal.
	if pathHasHistogramOnlyCall(path) || histFinder.found.Load() {
		if missing := p.strictMissingRemoteRead(); len(missing) > 0 {
			return nil, histogramFidelityError(missing)
		}
		return nil, nil
	}

	if aggFinder.Found > 0 {

		switch {
		// // If there was a single agg and that was us, then we're okay
		case (isAgg(node) || isBinaryExpr(node)) && aggFinder.Found == 1:
			break
		// If the aggregations are in a SubQuery; we can allow Subquery to run through NodeReplacerZz
		case isSubQuery(node):
			break
		default:
			return nil, nil
		}
	}

	// If there is more than 1 vector selector here and we are not a subquery
	// we can't combine as we don't know if those 2 selectors will for-sure
	// land on the same node
	if vecFinder.Found > 1 && !isSubQuery(node) {
		return nil, nil
	}

	// subtreeHasAt is true when at least one VectorSelector in this subtree has
	// an @ modifier. With @ in play we must NOT strip offsets or shift the
	// downstream request window: the downstream resolves `@ T offset O` into
	// sample[T-O] internally, so any rewrite that removes the offset or moves
	// the request range silently changes the lookup time.
	subtreeHasAt := timestampFinder.Found > 0

	// If the tree below us is not all the same offset, then we can't do anything below -- we'll need
	// to wait until further in execution where they all match
	var offset time.Duration

	// If we couldn't find an offset, then something is wrong-- lets skip
	// Also if there was an error, skip
	if !offsetFinder.Found || offsetFinder.Error != nil {
		return nil, nil
	}
	offset = offsetFinder.Offset

	// reqOffset is the time-shift applied to downstream request times so that
	// the engine, after restoring offsets on the synthesized VectorSelector,
	// looks up samples at the right timestamps. When the subtree has @, the
	// downstream already resolves @+offset, so we don't shift and the
	// synthesized node has no offset to re-apply.
	reqOffset := offset
	synthOffset := offset
	if subtreeHasAt {
		reqOffset = 0
		synthOffset = 0
	}

	// Function to recursivelt remove offset. This is needed as we're using
	// the node API to String() the query to downstreams. Promql's iterators require
	// that the time be the absolute time, whereas the API returns them based on the
	// range you ask for (with the offset being implicit).
	//
	// When the subtree has an @ modifier we keep offsets in the string: see
	// subtreeHasAt comment above.
	removeOffsetFn := func() error {
		if subtreeHasAt {
			return nil
		}
		_, err := parser.Walk(ctx, &promclient.OffsetRemover{}, s, node, nil, nil)
		return err
	}

	state := p.GetState()

	// queryRangeAt issues a step-aware downstream request for queryStr. When
	// the subtree below us pins evaluation to a single timestamp via @, the
	// result at every step is identical (step-invariant); in that case we
	// issue a single instant Query at the @ timestamp and replicate the
	// returned vector across each step in [s.Start, s.End]. This avoids
	// sending QueryRange with a pre-epoch sub-second start time, which the
	// upstream prometheus/common model.Time.UnmarshalJSON mis-decodes on
	// the way back (see api_query.go hasNegativeFractionalSecond). When the
	// subtree has no @, or it contains a Call whose result depends on the
	// evaluation timestamp even with @ pinning (timestamp, predict_linear,
	// time, etc. — see promql.AtModifierUnsafeFunctions), falls back to the
	// regular QueryRange.
	queryRangeAt := func(queryStr string) storage.SeriesSet {
		if subtreeHasAt && atTimestampFinder.Found && atUnsafeFinder.Found == 0 && s.Interval > 0 {
			at := timestamp.Time(atTimestampFinder.Timestamp)
			result := state.client.Query(ctx, queryStr, at)
			if err := result.Err(); err != nil {
				return result
			}
			// The instant query returns one sample per series at the @ time;
			// replicate each across the request's step grid. (Every series in
			// a SeriesSet is vector-shaped here — there is no Scalar/Matrix/
			// String ambiguity left at this layer.)
			return vectorToStepMatrix(result, s.Start.Add(-reqOffset), s.End.Add(-reqOffset), s.Interval)
		}
		return state.client.QueryRange(ctx, queryStr, v1.Range{
			Start: s.Start.Add(-reqOffset),
			End:   s.End.Add(-reqOffset),
			Step:  s.Interval,
		})
	}

	switch n := node.(type) {
	// Some AggregateExprs can be composed (meaning they are "reentrant". If the aggregation op
	// is reentrant/composable then we'll do so, otherwise we let it fall through to normal query mechanisms
	case *parser.AggregateExpr:
		// If the vector selector already has the data we can skip
		if vs, ok := n.Expr.(*parser.VectorSelector); ok {
			if vs.UnexpandedSeriesSet != nil {
				return nil, nil
			}
		}

		logrus.Debugf("AggregateExpr %v %s", n, n.Op)

		var result storage.SeriesSet
		var err error
		var lossy bool

		// Not all Aggregation functions are composable, so we'll do what we can
		switch n.Op {
		// All "reentrant" cases (meaning they can be done repeatedly and the outcome doesn't change)
		case parser.SUM, parser.MIN, parser.MAX, parser.TOPK, parser.BOTTOMK, parser.GROUP:
			removeOffsetFn()

			if s.Interval > 0 {
				result = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-reqOffset),
					End:   s.End.Add(-reqOffset),
					Step:  s.Interval,
				})
			} else {
				result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
			}

			if err != nil {
				return nil, err
			}
			result, lossy = containsLossyHistogram(result)
			if lossy {
				return nil, nil
			}

		// Convert avg into sum() / count()
		case parser.AVG:

			nameIncluded := false
			for _, g := range n.Grouping {
				if g == model.MetricNameLabel {
					nameIncluded = true
				}
			}

			if nameIncluded {
				replacedGrouping := make([]string, len(n.Grouping))
				for i, g := range n.Grouping {
					if g == model.MetricNameLabel {
						replacedGrouping[i] = metricNameWorkaroundLabel
					} else {
						replacedGrouping[i] = g
					}
				}

				return &parser.AggregateExpr{
					Op: parser.MAX,
					Expr: promclient.PreserveLabel(&parser.BinaryExpr{
						Op: parser.DIV,
						LHS: &parser.AggregateExpr{
							Op:       parser.SUM,
							Expr:     promclient.PreserveLabel(promclient.CloneExpr(n.Expr), model.MetricNameLabel, metricNameWorkaroundLabel),
							Param:    n.Param,
							Grouping: replacedGrouping,
							Without:  n.Without,
						},

						RHS: &parser.AggregateExpr{
							Op:       parser.COUNT,
							Expr:     promclient.PreserveLabel(promclient.CloneExpr(n.Expr), model.MetricNameLabel, metricNameWorkaroundLabel),
							Param:    n.Param,
							Grouping: replacedGrouping,
							Without:  n.Without,
						},
						VectorMatching: &parser.VectorMatching{Card: parser.CardOneToOne},
					}, metricNameWorkaroundLabel, model.MetricNameLabel),
					Grouping: n.Grouping,
					Without:  n.Without,
				}, nil

			}

			// Replace with sum() / count()
			return &parser.BinaryExpr{
				Op: parser.DIV,
				LHS: &parser.AggregateExpr{
					Op:       parser.SUM,
					Expr:     promclient.CloneExpr(n.Expr),
					Param:    n.Param,
					Grouping: n.Grouping,
					Without:  n.Without,
				},

				RHS: &parser.AggregateExpr{
					Op:       parser.COUNT,
					Expr:     promclient.CloneExpr(n.Expr),
					Param:    n.Param,
					Grouping: n.Grouping,
					Without:  n.Without,
				},
				VectorMatching: &parser.VectorMatching{Card: parser.CardOneToOne},
			}, nil

		// For count we simply need to change this to a sum over the data we get back
		case parser.COUNT:
			removeOffsetFn()

			if s.Interval > 0 {
				result = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-reqOffset),
					End:   s.End.Add(-reqOffset),
					Step:  s.Interval,
				})
			} else {
				result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
			}

			if err != nil {
				return nil, err
			}
			result, lossy = containsLossyHistogram(result)
			if lossy {
				return nil, nil
			}
			n.Op = parser.SUM

			// To aggregate count_values we simply sum(count_values(key, metric)) by (key)
		case parser.COUNT_VALUES:

			// First we must fetch the data into a vectorselector
			if s.Interval > 0 {
				result = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-reqOffset),
					End:   s.End.Add(-reqOffset),
					Step:  s.Interval,
				})
			} else {
				result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
			}

			if err != nil {
				return nil, err
			}
			result, lossy := containsLossyHistogram(result)
			if lossy {
				return nil, nil
			}

			ret := &parser.VectorSelector{OriginalOffset: synthOffset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = result

			// Replace with sum(count_values()) BY (label)
			return &parser.AggregateExpr{
				Op:       parser.SUM,
				Expr:     ret,
				Grouping: append(n.Grouping, n.Param.(*parser.StringLiteral).Val),
				Without:  n.Without,
			}, nil

		case parser.QUANTILE:
			// DO NOTHING
			// this caltulates an actual quantile over the resulting data
			// as such there is no way to reduce the load necessary here. If
			// the query is something like quantile(sum(foo)) then the inner aggregation
			// will reduce the required data

		// Both of these cases require some mechanism of knowing what labels to do the aggregation on.
		// WIthout that knowledge we require pulling all of the data in, so we do nothing
		case parser.STDDEV:
			// DO NOTHING
		case parser.STDVAR:
			// DO NOTHING

		// limitk(k, expr) and limit_ratio(r, expr) are NOT reentrant: the
		// engine picks k (or floor(r*N)) series by hash from the *full*
		// input vector, so pushing the aggregation independently into N
		// upstreams and unioning the results would pick a different,
		// possibly inconsistent, subset than evaluating against the union
		// directly. We let the engine evaluate locally over the raw matrix
		// data fetched via Querier.Select so the hash-based selection sees
		// the complete series set. (Listed here explicitly rather than
		// relying on default fall-through so the non-reentrant decision is
		// visible alongside the reentrant case list above.)
		case parser.LIMITK, parser.LIMIT_RATIO:
			// DO NOTHING

		}

		if result != nil {
			ret := &parser.VectorSelector{OriginalOffset: synthOffset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = result
			n.Expr = ret

			return n, nil
		}

	// Call is for things such as rate() etc. This can be sent directly to the
	// prometheus node to answer
	case *parser.Call:
		logrus.Debugf("call %v %v", n, n.Type())

		// absent and absent_over_time are difficult to implement at this layer; and as such we won't touch them
		// we'll do our NodeReplace at another node in the tree.
		//
		// label_join / label_replace / info are evaluated by the engine via
		// dedicated evalLabel{Join,Replace,Info} dispatchers that bypass the
		// FunctionCalls table and call ev.errorf/ev.error with a precise,
		// caller-facing message (e.g. "vector cannot contain metrics with
		// the same labelset"). Pushing them to a single downstream means the
		// error round-trips through ErrorWrap chains (target=…, servergroup=…)
		// before reaching the engine, mangling the exact wording — fine for
		// production, fatal for eval_fail tests. Let the engine handle these
		// locally by fetching args[0] via Querier.Select.
		switch n.Func.Name {
		case "absent", "absent_over_time",
			"label_join", "label_replace", "info":
			return nil, nil
		}

		// For all the Call's we actually will work on, we need to remove the offset
		removeOffsetFn()

		var result storage.SeriesSet
		var err error
		if s.Interval > 0 {
			result = queryRangeAt(n.String())
		} else {
			result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
		}

		if err != nil {
			return nil, err
		}
		result, lossy := containsLossyHistogram(result)
		if lossy {
			return nil, nil
		}

		// For range queries, fill StaleNaN at step timestamps the downstream
		// did not return a value for. The engine's per-step VectorSelector
		// eval uses ev.lookbackDelta (5m default, not our ret.LookbackDelta
		// hint) when peeking at the previous sample, so an isolated step
		// sample from a sparse range output (e.g. present_over_time returning
		// 1 at one step only) otherwise bleeds forward into every later step
		// within the lookback window.
		if s.Interval > 0 {
			startMs := timestamp.FromTime(s.Start.Add(-reqOffset))
			endMs := timestamp.FromTime(s.End.Add(-reqOffset))
			result = fillStaleNaNGaps(result, startMs, endMs, int64(s.Interval/time.Millisecond))
		}

		ret := &parser.VectorSelector{OriginalOffset: synthOffset}
		if s.Interval > 0 {
			ret.LookbackDelta = s.Interval - time.Duration(1)
		}
		ret.UnexpandedSeriesSet = result

		// Some functions require specific handling which we'll catch here
		switch n.Func.Name {
		// the "scalar()" function is a bit tricky. It can return a scalar or a vector.
		// So to handle this instead of returning the vector directly (as its just the values selected)
		// we can set it as the args (the vector of data) and the promql engine handles the types properly
		case "scalar":
			n.Args[0] = ret
			return n, nil
		// the functions of sort() and sort_desc() need whole results to calculate.
		case "sort", "sort_desc":
			return &parser.Call{
				Func: n.Func,
				Args: []parser.Expr{ret},
			}, nil
		}

		return ret, nil

	// If we are simply fetching a Vector then we can fetch the data using the same step that
	// the query came in as (reducing the amount of data we need to fetch)
	case *parser.VectorSelector:
		// If the vector selector already has the data we can skip
		if n.UnexpandedSeriesSet != nil {
			return nil, nil
		}

		// Check if this VectorSelector is below a MatrixSelector.
		// If we hit this someone is asking for a matrix directly, if so then we don't
		// have anyway to ask for less-- since this is exactly what they are asking for
		if len(path) > 0 {
			if _, ok := path[len(path)-1].(*parser.MatrixSelector); ok {
				return nil, nil
			}
		}

		logrus.Debugf("VectorSelector: %v", n)
		removeOffsetFn()

		var result storage.SeriesSet
		origLookback := n.LookbackDelta
		if s.Interval > 0 {
			n.LookbackDelta = s.Interval - time.Duration(1)
			result = state.client.QueryRange(ctx, n.String(), v1.Range{
				Start: s.Start.Add(-reqOffset),
				End:   s.End.Add(-reqOffset),
				Step:  s.Interval,
			})
		} else {
			result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
		}

		if err := result.Err(); err != nil {
			return nil, err
		}
		result, lossy := containsLossyHistogram(result)
		if lossy {
			// We abandoned the pushdown — restore the original LookbackDelta
			// so the engine's local eval uses the default lookback when it
			// calls Querier.Select instead of the tighter step-minus-1
			// window we set above (which would otherwise drop boundary
			// samples like a load at t=0 with a range query starting at 0).
			n.LookbackDelta = origLookback
			return nil, nil
		}

		if n.Timestamp != nil {
			// Downstream resolved @ T (and any offset) when evaluating
			// n.String(); the result is step-invariant. Replace with a flat
			// VectorSelector whose samples sit at the request timestamps so
			// the engine looks them up by ts directly instead of reapplying
			// @ T - offset to a sample set that's already pinned.
			//
			// Preserve Name and LabelMatchers: functions like absent() read
			// these from the (already-resolved) VectorSelector AST node to
			// synthesize output labels (createLabelsForAbsentFunction). The
			// matchers are inert for data lookup at this point — the
			// downstream already returned exactly the right series — but
			// dropping them would mean absent(foo{job="x"} @ T) returns
			// `{} 1` instead of `{job="x"} 1`.
			ret := &parser.VectorSelector{
				Name:           n.Name,
				LabelMatchers:  n.LabelMatchers,
				OriginalOffset: synthOffset,
			}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = result
			return ret, nil
		}
		n.OriginalOffset = offset
		n.UnexpandedSeriesSet = result
		return n, nil

	// If we hit this someone is asking for a matrix directly, if so then we don't
	// have anyway to ask for less-- since this is exactly what they are asking for
	case *parser.MatrixSelector:
		// DO NOTHING

	// For Subquery Expressions; we basically want to replace them with our own statement (separate interval, step, etc.)
	// Note: since we are replacing this with another query we can get some value differences as this sends larger sub-queries
	// downstream (which may have access to less data, and promql has some weird heuristics on how it calculates values on step)
	case *parser.SubqueryExpr:
		logrus.Debugf("SubqueryExpr: %v", n)

		subEvalStmt := *s
		subEvalStmt.Expr = n.Expr

		// If the subquery has an @ modifier its evaluation is pinned to that
		// timestamp and the outer eval window is irrelevant.
		var subEnd time.Time
		if n.Timestamp != nil {
			subEnd = timestamp.Time(*n.Timestamp).Add(-n.Offset)
		} else {
			subEnd = s.End.Add(-n.Offset)
		}
		subEvalStmt.End = subEnd

		if n.Step == 0 {
			subEvalStmt.Interval = time.Duration(p.NoStepSubqueryIntervalFn(durationMilliseconds(n.Range))) * time.Millisecond
		} else {
			subEvalStmt.Interval = n.Step
		}

		var subStart time.Time
		if n.Timestamp != nil {
			subStart = subEnd.Add(-n.Range)
		} else {
			subStart = s.Start.Add(-n.Offset).Add(-n.Range)
		}
		subEvalStmt.Start = subStart.Truncate(subEvalStmt.Interval)
		if subEvalStmt.Start.Before(subStart) {
			subEvalStmt.Start = subEvalStmt.Start.Add(subEvalStmt.Interval)
		}

		newN, err := parser.Inspect(ctx, &subEvalStmt, func(parser.Node, []parser.Node) error { return nil }, p.NodeReplacer)
		if err != nil {
			return nil, err
		}

		if newN != nil {
			n.Expr = newN.(parser.Expr)
			return n, nil
		}

	// BinaryExprs *can* be sent untouched to downstreams assuming there is no actual interaction between LHS/RHS
	// these are relatively rare -- as things like `sum(foo) > 2` would *not* be viable as `sum(foo)` could
	// potentially require multiple servergroups to generate the correct response.
	// From inspection there are only 3 specific types where this sort of replacement is "safe" (assuming one side is a literal)
	// 	`VectorSector`
	// 	`AggregateExpr` (Max, Min, TopK, BottomK only -- and only if re-combined)
	case *parser.BinaryExpr:
		logrus.Debugf("BinaryExpr: %v", n)

		// vectorBinaryExpr will send the node as a query to the downstream and return an expanded VectorSelector
		vectorBinaryExpr := func(vs *parser.VectorSelector) (parser.Node, error) {
			logrus.Debugf("BinaryExpr (VectorSelector + Literal): %v", n)
			removeOffsetFn()

			var result storage.SeriesSet
			if s.Interval > 0 {
				vs.LookbackDelta = s.Interval - time.Duration(1)
				result = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-reqOffset),
					End:   s.End.Add(-reqOffset),
					Step:  s.Interval,
				})
			} else {
				result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
			}

			if err := result.Err(); err != nil {
				return nil, err
			}
			result, lossy := containsLossyHistogram(result)
			if lossy {
				return nil, nil
			}

			ret := &parser.VectorSelector{OriginalOffset: synthOffset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = result
			return ret, nil
		}

		// aggregateBinaryExpr will send the node as a query to the downstream and
		// replace the aggregate expr with the resulting data. This will cause the aggregation
		// (min, max, topk, bottomk) to be re-run against the expression.
		aggregateBinaryExpr := func(agg *parser.AggregateExpr) (parser.Node, error) {
			logrus.Debugf("BinaryExpr (AggregateExpr + Literal): %v", n)

			removeOffsetFn()

			var result storage.SeriesSet

			if s.Interval > 0 {
				result = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-reqOffset),
					End:   s.End.Add(-reqOffset),
					Step:  s.Interval,
				})
			} else {
				result = state.client.Query(ctx, n.String(), s.Start.Add(-reqOffset))
			}
			if err := result.Err(); err != nil {
				return nil, err
			}
			result, lossy := containsLossyHistogram(result)
			if lossy {
				return nil, nil
			}

			ret := &parser.VectorSelector{OriginalOffset: synthOffset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = result

			agg.Expr = ret
			return agg, nil
		}

		// Only valid if the other side is either `NumberLiteral` or `StringLiteral`
		this := n.LHS
		other := n.RHS
		literal := promclient.ExprIsLiteral(promclient.UnwrapExpr(this))
		if !literal {
			this = n.RHS
			other = n.LHS
			literal = promclient.ExprIsLiteral(promclient.UnwrapExpr(this))
		}
		// If one side is a literal lets check
		if literal {
			switch otherTyped := other.(type) {
			case *parser.VectorSelector:
				return vectorBinaryExpr(otherTyped)
			case *parser.AggregateExpr:
				switch otherTyped.Op {
				case parser.MIN, parser.MAX, parser.TOPK, parser.BOTTOMK:
					return aggregateBinaryExpr(otherTyped)
				}
			}
		}

	default:
		logrus.Debugf("default %v %s", n, reflect.TypeOf(n))

	}
	return nil, nil
}

func durationMilliseconds(d time.Duration) int64 {
	return int64(d / (time.Millisecond / time.Nanosecond))
}

// fillStaleNaNGaps inserts StaleNaN markers at the step timestamps where the
// downstream's range response did not include a sample. We need this because
// promxy substitutes a *parser.Call (e.g. present_over_time, last_over_time)
// with a synthetic VectorSelector whose UnexpandedSeriesSet exposes the
// downstream-computed samples. When the engine re-evaluates that VectorSelector
// it uses the engine-wide lookback (default 5m) to find samples — even though
// the substituted node represents the call's already-computed step-pinned
// output. Without explicit "no value at this step" markers, an isolated sample
// at one step T_k bleeds forward to every subsequent step within the lookback,
// turning sparse outputs (present_over_time returning 1 at only one step) into
// dense ones. StaleNaN at the empty step timestamps tells vectorSelectorSingle
// to bail out at that step (see promql.IsStaleNaN check in engine.go).
//
// startTs/endTs/interval are in milliseconds. startTs and endTs are inclusive.
// If interval is <= 0 (instant query) ss is returned unchanged. Otherwise the
// set is materialized into a fresh, re-iterable copy with the StaleNaN markers
// added — the source cursor is consumed, and the result still flows on to
// UnexpandedSeriesSet.
func fillStaleNaNGaps(ss storage.SeriesSet, startTs, endTs, interval int64) storage.SeriesSet {
	if interval <= 0 {
		return ss
	}
	stale := math.Float64frombits(value.StaleNaN)
	var out []storage.Series
	for ss.Next() {
		s := ss.At()
		lbls := s.Labels().Copy()
		var samples []chunks.Sample
		present := make(map[int64]struct{})
		it := s.Iterator(nil)
		for vt := it.Next(); vt != chunkenc.ValNone; vt = it.Next() {
			switch vt {
			case chunkenc.ValFloat:
				t, v := it.At()
				samples = append(samples, promapi.FloatSample(t, v))
				present[t] = struct{}{}
			case chunkenc.ValHistogram, chunkenc.ValFloatHistogram:
				t, fh := it.AtFloatHistogram(nil)
				samples = append(samples, promapi.HistogramSample(t, fh))
				present[t] = struct{}{}
			}
		}
		if err := it.Err(); err != nil {
			return promapi.NewSeriesSet(nil, ss.Warnings(), err)
		}

		// Bound the fill defensively: a bogus start/end from upstream
		// shouldn't make us allocate a giant slice. If the eval window is
		// far larger than the actual returned data, skip the fill — the
		// existing samples stay correct (the engine's lookback can still
		// misbehave, but that's strictly no worse than before).
		expected := (endTs-startTs)/interval + 1
		if expected > 0 && expected <= int64(len(present))+10_000 {
			for ts := startTs; ts <= endTs; ts += interval {
				if _, ok := present[ts]; ok {
					continue
				}
				samples = append(samples, promapi.FloatSample(ts, stale))
			}
			// promapi.NewSeries' list iterator must walk samples in
			// timestamp order; the appended StaleNaN points can sit out of
			// order relative to the originals.
			sortSamplesByTime(samples)
		}
		out = append(out, promapi.NewSeries(lbls, samples))
	}
	return promapi.NewSeriesSet(out, ss.Warnings(), ss.Err())
}

// sortSamplesByTime sorts samples by timestamp using insertion sort — a step
// range is typically a handful of points, so a stdlib sort.Slice would pay
// disproportionate cost for the closure call.
func sortSamplesByTime(vs []chunks.Sample) {
	for i := 1; i < len(vs); i++ {
		for j := i; j > 0 && vs[j-1].T() > vs[j].T(); j-- {
			vs[j-1], vs[j] = vs[j], vs[j-1]
		}
	}
}
