package proxystorage

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/promql/parser"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb"
	"github.com/prometheus/prometheus/tsdb/index"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/remote"

	"github.com/jacksontj/promxy/pkg/logging"
	"github.com/jacksontj/promxy/pkg/promhttputil"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/proxyquerier"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// metricNameWorkaroundLabel is a workaround from https://github.com/jacksontj/promxy/issues/274
const metricNameWorkaroundLabel = "__name"

type proxyStorageState struct {
	sgs            []*servergroup.ServerGroup
	client         promclient.API
	cfg            *proxyconfig.Config
	remoteStorage  *remote.Storage
	appender       storage.Appender
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
	// We call close if the new one is nil, or if the appenders don't match
	if n == nil || p.appender != n.appender {
		if p.appenderCloser != nil {
			p.appenderCloser()
		}
	}
}

// NewProxyStorage creates a new ProxyStorage
func NewProxyStorage(NoStepSubqueryIntervalFn func(rangeMillis int64) int64) (*ProxyStorage, error) {
	return &ProxyStorage{NoStepSubqueryIntervalFn: NoStepSubqueryIntervalFn}, nil
}

// ProxyStorage implements prometheus' Storage interface
type ProxyStorage struct {
	NoStepSubqueryIntervalFn func(rangeMillis int64) int64
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

	multiApi, err := promclient.NewMultiAPI(apis, model.TimeFromUnix(0), nil, len(apis), false)
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
		} else {
			remote := remote.NewStorage(logging.NewLogger(logrus.WithField("component", "remote_write").Logger), func() (int64, error) { return 0, nil }, 1*time.Second)
			if err := remote.ApplyConfig(&c.PromConfig); err != nil {
				return err
			}
			newState.remoteStorage = remote
			newState.appenderCloser = remote.Close
		}

		// Whether old or new, update the appender
		var err error
		newState.appender, err = newState.remoteStorage.Appender()
		if err != nil {
			return err
		}

	} else {
		newState.appender = &appenderStub{}
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
func (p *ProxyStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	state := p.GetState()
	return &proxyquerier.ProxyQuerier{
		Ctx:    ctx,
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

// Appender returns a new appender against the storage.
func (p *ProxyStorage) Appender(context.Context) storage.Appender {
	return p.GetState().appender
}

// Close releases the resources of the Querier.
func (p *ProxyStorage) Close() error { return nil }

// ChunkQuerier returns a new ChunkQuerier on the storage.
func (p *ProxyStorage) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return nil, errors.New("not implemented")
}

// ExemplarQuerier returns a new ExemplarQuerier on the storage.
func (p *ProxyStorage) ExemplarQuerier(ctx context.Context) (storage.ExemplarQuerier, error) {
	return nil, errors.New("not implemented")
}

// Implement web.LocalStorage
func (p *ProxyStorage) CleanTombstones() (err error)                         { return nil }
func (p *ProxyStorage) Delete(mint, maxt int64, ms ...*labels.Matcher) error { return nil }
func (p *ProxyStorage) Snapshot(dir string, withHead bool) error             { return nil }
func (p *ProxyStorage) Stats(statsByLabelName string) (*tsdb.Stats, error) {
	return &tsdb.Stats{IndexPostingStats: &index.PostingsStats{}}, nil
}
func (p *ProxyStorage) WALReplayStatus() (tsdb.WALReplayStatus, error) {
	return tsdb.WALReplayStatus{}, errors.New("not implemented")
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

	// If we are a child of a subquery; we just skip replacement (since it already did a nodereplacer for those)
	for _, n := range path {
		if isSubQuery(n) {
			return nil, nil
		}
	}

	// If there is a child that is an aggregator we cannot do anything (as they have their own
	// rules around combining). We'll skip this node and let a lower layer take this on
	aggFinder := &BooleanFinder{Func: isAgg}
	offsetFinder := &OffsetFinder{}
	vecFinder := &BooleanFinder{Func: isVectorSelector}
	timestampFinder := &BooleanFinder{Func: hasTimestamp}

	visitor := NewMultiVisitor([]parser.Visitor{aggFinder, offsetFinder, vecFinder, timestampFinder})

	if _, err := parser.Walk(ctx, visitor, s, node, nil, nil); err != nil {
		return nil, err
	}

	if aggFinder.Found > 0 {
		// If there was a single agg and that was us, then we're okay
		if !((isAgg(node) || isSubQuery(node) || isBinaryExpr(node)) && aggFinder.Found == 1) {
			return nil, nil
		}
	}

	// If there is more than 1 vector selector here and we are not a subquery
	// we can't combine as we don't know if those 2 selectors will for-sure
	// land on the same node
	if vecFinder.Found > 1 && !isSubQuery(node) {
		return nil, nil
	}

	// If the subtree has an at modifier (e.g. "@ 500") we don't currently support those so we'll skip
	// this is only enabled in the promql engine for tests, but if we want to support this generally we'll
	// have to fix this
	if timestampFinder.Found > 0 {
		return nil, nil
	}

	// If the tree below us is not all the same offset, then we can't do anything below -- we'll need
	// to wait until further in execution where they all match
	var offset time.Duration

	// If we couldn't find an offset, then something is wrong-- lets skip
	// Also if there was an error, skip
	if !offsetFinder.Found || offsetFinder.Error != nil {
		return nil, nil
	}
	offset = offsetFinder.Offset

	// Function to recursivelt remove offset. This is needed as we're using
	// the node API to String() the query to downstreams. Promql's iterators require
	// that the time be the absolute time, whereas the API returns them based on the
	// range you ask for (with the offset being implicit)
	removeOffsetFn := func() error {
		_, err := parser.Walk(ctx, &OffsetRemover{}, s, node, nil, nil)
		return err
	}

	state := p.GetState()
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

		var result model.Value
		var warnings v1.Warnings
		var err error

		// Not all Aggregation functions are composable, so we'll do what we can
		switch n.Op {
		// All "reentrant" cases (meaning they can be done repeatedly and the outcome doesn't change)
		case parser.SUM, parser.MIN, parser.MAX, parser.TOPK, parser.BOTTOMK, parser.GROUP:
			removeOffsetFn()

			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}

			if err != nil {
				return nil, err
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
					Expr: PreserveLabel(&parser.BinaryExpr{
						Op: parser.DIV,
						LHS: &parser.AggregateExpr{
							Op:       parser.SUM,
							Expr:     PreserveLabel(CloneExpr(n.Expr), model.MetricNameLabel, metricNameWorkaroundLabel),
							Param:    n.Param,
							Grouping: replacedGrouping,
							Without:  n.Without,
						},

						RHS: &parser.AggregateExpr{
							Op:       parser.COUNT,
							Expr:     PreserveLabel(CloneExpr(n.Expr), model.MetricNameLabel, metricNameWorkaroundLabel),
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
					Expr:     CloneExpr(n.Expr),
					Param:    n.Param,
					Grouping: n.Grouping,
					Without:  n.Without,
				},

				RHS: &parser.AggregateExpr{
					Op:       parser.COUNT,
					Expr:     CloneExpr(n.Expr),
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
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}

			if err != nil {
				return nil, err
			}
			n.Op = parser.SUM

			// To aggregate count_values we simply sum(count_values(key, metric)) by (key)
		case parser.COUNT_VALUES:

			// First we must fetch the data into a vectorselector
			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}

			if err != nil {
				return nil, err
			}

			iterators := promclient.IteratorsForValue(result)

			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &parser.VectorSelector{OriginalOffset: offset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)

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

		}

		if result != nil {
			iterators := promclient.IteratorsForValue(result)

			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &parser.VectorSelector{OriginalOffset: offset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)
			n.Expr = ret

			return n, nil
		}

	// Call is for things such as rate() etc. This can be sent directly to the
	// prometheus node to answer
	case *parser.Call:
		logrus.Debugf("call %v %v", n, n.Type())

		// absent and absent_over_time are difficult to implement at this layer; and as such we won't touch them
		// we'll do our NodeReplace at another node in the tree
		switch n.Func.Name {
		case "absent", "absent_over_time":
			return nil, nil
		}

		// For all the Call's we actually will work on, we need to remove the offset
		removeOffsetFn()

		var result model.Value
		var warnings v1.Warnings
		var err error
		if s.Interval > 0 {
			result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
				Start: s.Start.Add(-offset),
				End:   s.End.Add(-offset),
				Step:  s.Interval,
			})
		} else {
			result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
		}

		if err != nil {
			return nil, err
		}

		iterators := promclient.IteratorsForValue(result)
		series := make([]storage.Series, len(iterators))
		for i, iterator := range iterators {
			series[i] = &proxyquerier.Series{iterator}
		}

		ret := &parser.VectorSelector{OriginalOffset: offset}
		if s.Interval > 0 {
			ret.LookbackDelta = s.Interval - time.Duration(1)
		}
		ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)

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
		if n.Timestamp != nil {
			return nil, nil
		}
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

		var result model.Value
		var warnings v1.Warnings
		var err error
		if s.Interval > 0 {
			n.LookbackDelta = s.Interval - time.Duration(1)
			result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
				Start: s.Start.Add(-offset),
				End:   s.End.Add(-offset),
				Step:  s.Interval,
			})
		} else {
			result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
		}

		if err != nil {
			return nil, err
		}

		iterators := promclient.IteratorsForValue(result)
		series := make([]storage.Series, len(iterators))
		for i, iterator := range iterators {
			series[i] = &proxyquerier.Series{iterator}
		}
		n.OriginalOffset = offset
		n.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)
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
		subEvalStmt.End = subEvalStmt.End.Add(-n.Offset)
		//subEvalStmt.Start = subEvalStmt.End.Add(-n.Range)

		if n.Step == 0 {
			subEvalStmt.Interval = time.Duration(p.NoStepSubqueryIntervalFn(durationMilliseconds(n.Range))) * time.Millisecond
		} else {
			subEvalStmt.Interval = n.Step
		}

		subEvalStmt.Start = s.Start.Add(-n.Offset).Add(-n.Range).Truncate(subEvalStmt.Interval)
		if subEvalStmt.Start.Before(s.Start.Add(-n.Offset).Add(-n.Range)) {
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

			var result model.Value
			var warnings v1.Warnings
			var err error
			if s.Interval > 0 {
				vs.LookbackDelta = s.Interval - time.Duration(1)
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}

			if err != nil {
				return nil, err
			}

			iterators := promclient.IteratorsForValue(result)
			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &parser.VectorSelector{OriginalOffset: offset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)
			return ret, nil
		}

		// aggregateBinaryExpr will send the node as a query to the downstream and
		// replace the aggregate expr with the resulting data. This will cause the aggregation
		// (min, max, topk, bottomk) to be re-run against the expression.
		aggregateBinaryExpr := func(agg *parser.AggregateExpr) error {
			logrus.Debugf("BinaryExpr (AggregateExpr + Literal): %v", n)

			removeOffsetFn()

			var (
				result   model.Value
				warnings v1.Warnings
				err      error
			)

			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}
			if err != nil {
				return err
			}

			iterators := promclient.IteratorsForValue(result)

			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &parser.VectorSelector{OriginalOffset: offset}
			if s.Interval > 0 {
				ret.LookbackDelta = s.Interval - time.Duration(1)
			}
			ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)

			agg.Expr = ret
			return nil
		}

		// Only valid if the other side is either `NumberLiteral` or `StringLiteral`
		this := n.LHS
		other := n.RHS
		literal := ExprIsLiteral(UnwrapExpr(this))
		if !literal {
			this = n.RHS
			other = n.LHS
			literal = ExprIsLiteral(UnwrapExpr(this))
		}
		// If one side is a literal lets check
		if literal {
			switch otherTyped := other.(type) {
			case *parser.VectorSelector:
				return vectorBinaryExpr(otherTyped)
			case *parser.AggregateExpr:
				switch otherTyped.Op {
				case parser.MIN, parser.MAX, parser.TOPK, parser.BOTTOMK:
					if err := aggregateBinaryExpr(otherTyped); err != nil {
						return nil, err
					}
					return n, nil
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
