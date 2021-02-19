package proxystorage

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
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

const MetricNameWorkaroundLabel = "__name"

type proxyStorageState struct {
	sgs            []*servergroup.ServerGroup
	client         promclient.API
	cfg            *proxyconfig.PromxyConfig
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
	// We call close if the new one is nil, or if the appanders don't match
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
		cfg: &c.PromxyConfig,
	}
	for i, sgCfg := range c.ServerGroups {
		tmp := servergroup.New()
		if err := tmp.ApplyConfig(sgCfg); err != nil {
			failed = true
			logrus.Errorf("Error applying config to server group: %s", err)
		}
		newState.sgs[i] = tmp
		apis[i] = tmp
	}
	newState.client = promclient.NewTimeTruncate(promclient.NewMultiAPI(apis, model.TimeFromUnix(0), nil, len(apis)))

	if failed {
		newState.Cancel(nil)
		return fmt.Errorf("error applying config to one or more server group(s)")
	}

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
	if oldState != nil && oldState.appender != newState.appender {
		oldState.Cancel(newState) // Cancel the old one
	}

	return nil
}

// Querier returns a new Querier on the storage.
func (p *ProxyStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	state := p.GetState()
	return &proxyquerier.ProxyQuerier{
		ctx,
		timestamp.Time(mint).UTC(),
		timestamp.Time(maxt).UTC(),
		state.client,

		state.cfg,
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

func (p *ProxyStorage) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return nil, errors.New("not implemented")
}

// Implement web.LocalStorage
func (p *ProxyStorage) CleanTombstones() (err error)                         { return nil }
func (p *ProxyStorage) Delete(mint, maxt int64, ms ...*labels.Matcher) error { return nil }
func (p *ProxyStorage) Snapshot(dir string, withHead bool) error             { return nil }
func (p *ProxyStorage) Stats(statsByLabelName string) (*tsdb.Stats, error) {
	return &tsdb.Stats{IndexPostingStats: &index.PostingsStats{}}, nil
}

// NodeReplacer replaces promql Nodes with more efficient-to-fetch ones. This works by taking lower-layer
// chunks of the query, farming them out to prometheus hosts, then stitching the results back together.
// An example would be a sum, we can sum multiple sums and come up with the same result -- so we do.
// There are a few ground rules for this:
//      - Children cannot be AggregateExpr: aggregates have their own combining logic, so its not safe to send a subquery with additional aggregations
//      - offsets within the subtree must match: if they don't then we'll get mismatched data, so we wait until we are far enough down the tree that they converge
//      - Don't reduce accuracy/granularity: the intention of this is to get the correct data faster, meaning correctness overrules speed.
func (p *ProxyStorage) NodeReplacer(ctx context.Context, s *parser.EvalStmt, node parser.Node, path []parser.Node) (parser.Node, error) {
	isAgg := func(node parser.Node) bool {
		_, ok := node.(*parser.AggregateExpr)
		return ok
	}

	isSubQuery := func(node parser.Node) bool {
		_, ok := node.(*parser.SubqueryExpr)
		return ok
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

	visitor := NewMultiVisitor([]parser.Visitor{aggFinder, offsetFinder})

	if _, err := parser.Walk(ctx, visitor, s, node, nil, nil); err != nil {
		return nil, err
	}

	if aggFinder.Found > 0 {
		// If there was a single agg and that was us, then we're okay
		if !((isAgg(node) || isSubQuery(node)) && aggFinder.Found == 1) {
			return nil, nil
		}
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
		logrus.Debugf("AggregateExpr %v %s", n, n.Op)

		var result model.Value
		var warnings v1.Warnings
		var err error

		// Not all Aggregation functions are composable, so we'll do what we can
		switch n.Op {
		// All "reentrant" cases (meaning they can be done repeatedly and the outcome doesn't change)
		case parser.SUM, parser.MIN, parser.MAX, parser.TOPK, parser.BOTTOMK:
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
				return nil, errors.Cause(err)
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
						replacedGrouping[i] = MetricNameWorkaroundLabel
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
							Expr:     PreserveLabel(CloneExpr(n.Expr), model.MetricNameLabel, MetricNameWorkaroundLabel),
							Param:    n.Param,
							Grouping: replacedGrouping,
							Without:  n.Without,
						},

						RHS: &parser.AggregateExpr{
							Op:       parser.COUNT,
							Expr:     PreserveLabel(CloneExpr(n.Expr), model.MetricNameLabel, MetricNameWorkaroundLabel),
							Param:    n.Param,
							Grouping: replacedGrouping,
							Without:  n.Without,
						},
						VectorMatching: &parser.VectorMatching{Card: parser.CardOneToOne},
					}, MetricNameWorkaroundLabel, model.MetricNameLabel),
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
				return nil, errors.Cause(err)
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
				return nil, errors.Cause(err)
			}

			iterators := promclient.IteratorsForValue(result)

			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &parser.VectorSelector{Offset: offset}
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

			ret := &parser.VectorSelector{Offset: offset}
			ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)
			n.Expr = ret

			return n, nil
		}

	// Call is for things such as rate() etc. This can be sent directly to the
	// prometheus node to answer
	case *parser.Call:
		logrus.Debugf("call %v %v", n, n.Type())
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
			return nil, errors.Cause(err)
		}

		iterators := promclient.IteratorsForValue(result)
		series := make([]storage.Series, len(iterators))
		for i, iterator := range iterators {
			series[i] = &proxyquerier.Series{iterator}
		}

		ret := &parser.VectorSelector{Offset: offset}
		ret.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)

		// the "scalar()" function is a bit tricky. It can return a scalar or a vector.
		// So to handle this instead of returning the vector directly (as its just the values selected)
		// we can set it as the args (the vector of data) and the promql engine handles the types properly
		if n.Func.Name == "scalar" {
			n.Args[0] = ret
			return nil, nil
		}
		return ret, nil

	// If we are simply fetching a Vector then we can fetch the data using the same step that
	// the query came in as (reducing the amount of data we need to fetch)
	case *parser.VectorSelector:
		// If the vector selector already has the data we can skip
		if n.UnexpandedSeriesSet != nil {
			return nil, nil
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
			return nil, errors.Cause(err)
		}

		iterators := promclient.IteratorsForValue(result)
		series := make([]storage.Series, len(iterators))
		for i, iterator := range iterators {
			series[i] = &proxyquerier.Series{iterator}
		}
		n.Offset = offset
		n.UnexpandedSeriesSet = proxyquerier.NewSeriesSet(series, promhttputil.WarningsConvert(warnings), err)

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
			subEvalStmt.Start.Add(subEvalStmt.Interval)
		}

		newN, err := parser.Inspect(ctx, &subEvalStmt, func(parser.Node, []parser.Node) error { return nil }, p.NodeReplacer)
		if err != nil {
			return nil, err
		}

		if newN != nil {
			n.Expr = newN.(parser.Expr)
			return n, nil
		}

	default:
		logrus.Debugf("default %v %s", n, reflect.TypeOf(n))

	}
	return nil, nil
}

func durationMilliseconds(d time.Duration) int64 {
	return int64(d / (time.Millisecond / time.Nanosecond))
}
