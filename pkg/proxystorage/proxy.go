package proxystorage

import (
	"context"
	"fmt"
	"reflect"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/promhttputil"
	"github.com/jacksontj/promxy/pkg/remote"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/proxyquerier"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

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
	for _, sg := range p.sgs {
		<-sg.Ready
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
func NewProxyStorage() (*ProxyStorage, error) {
	return &ProxyStorage{}, nil
}

// ProxyStorage implements prometheus' Storage interface
type ProxyStorage struct {
	state atomic.Value
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
		return fmt.Errorf("Error Applying Config to one or more server group(s)")
	}

	// Check for remote_write (for appender)
	if c.PromConfig.RemoteWriteConfigs != nil {
		if oldState.remoteStorage != nil {
			if err := oldState.remoteStorage.ApplyConfig(&c.PromConfig); err != nil {
				return err
			}
			// if it was an appenderstub we just need to replace
		} else {
			// TODO: configure path?
			remote := remote.NewStorage(nil, func() (int64, error) { return 0, nil }, 1*time.Second)
			if err := remote.ApplyConfig(&c.PromConfig); err != nil {
				return err
			}
			newState.remoteStorage = remote
			var err error
			newState.appender, err = remote.Appender()
			if err != nil {
				return errors.Wrap(err, "unable to create remote_write appender")
			}
			newState.appenderCloser = remote.Close
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
func (p *ProxyStorage) Appender() (storage.Appender, error) {
	state := p.GetState()
	return state.appender, nil
}

// Close releases the resources of the Querier.
func (p *ProxyStorage) Close() error { return nil }

// NodeReplacer replaces promql Nodes with more efficient-to-fetch ones. This works by taking lower-layer
// chunks of the query, farming them out to prometheus hosts, then stitching the results back together.
// An example would be a sum, we can sum multiple sums and come up with the same result -- so we do.
// There are a few ground rules for this:
//      - Children cannot be AggregateExpr: aggregates have their own combining logic, so its not safe to send a subquery with additional aggregations
//      - offsets within the subtree must match: if they don't then we'll get mismatched data, so we wait until we are far enough down the tree that they converge
//      - Don't reduce accuracy/granularity: the intention of this is to get the correct data faster, meaning correctness overrules speed.
func (p *ProxyStorage) NodeReplacer(ctx context.Context, s *promql.EvalStmt, node promql.Node) (promql.Node, error) {

	isAgg := func(node promql.Node) bool {
		_, ok := node.(*promql.AggregateExpr)
		return ok
	}

	// If there is a child that is an aggregator we cannot do anything (as they have their own
	// rules around combining). We'll skip this node and let a lower layer take this on
	aggFinder := &BooleanFinder{Func: isAgg}
	offsetFinder := &OffsetFinder{}

	visitor := &MultiVisitor{[]promql.Visitor{aggFinder, offsetFinder}}

	if _, err := promql.Walk(ctx, visitor, s, node, nil, nil); err != nil {
		return nil, err
	}

	if aggFinder.Found > 0 {
		// If there was a single agg and that was us, then we're okay
		if !(isAgg(node) && aggFinder.Found == 1) {
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
	// TODO: rename
	removeOffset := func() error {
		_, err := promql.Walk(ctx, &OffsetRemover{}, s, node, nil, nil)
		return err
	}

	state := p.GetState()
	switch n := node.(type) {
	// Some AggregateExprs can be composed (meaning they are "reentrant". If the aggregation op
	// is reentrant/composable then we'll do so, otherwise we let it fall through to normal query mechanisms
	case *promql.AggregateExpr:
		logrus.Debugf("AggregateExpr %v", n)

		var result model.Value
		var warnings api.Warnings
		var err error

		// Not all Aggregation functions are composable, so we'll do what we can
		switch n.Op {
		// All "reentrant" cases (meaning they can be done repeatedly and the outcome doesn't change)
		case promql.ItemSum, promql.ItemMin, promql.ItemMax, promql.ItemTopK, promql.ItemBottomK:
			removeOffset()

			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset - promql.LookbackDelta),
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
		case promql.ItemAvg:
			// Replace with sum() / count()
			return &promql.BinaryExpr{
				Op: promql.ItemDIV,
				LHS: &promql.AggregateExpr{
					Op:       promql.ItemSum,
					Expr:     n.Expr,
					Param:    n.Param,
					Grouping: n.Grouping,
					Without:  n.Without,
				},

				RHS: &promql.AggregateExpr{
					Op:       promql.ItemCount,
					Expr:     n.Expr,
					Param:    n.Param,
					Grouping: n.Grouping,
					Without:  n.Without,
				},
				VectorMatching: &promql.VectorMatching{Card: promql.CardOneToOne},
			}, nil

		// For count we simply need to change this to a sum over the data we get back
		case promql.ItemCount:
			removeOffset()

			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset - promql.LookbackDelta),
					End:   s.End.Add(-offset),
					Step:  s.Interval,
				})
			} else {
				result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
			}

			if err != nil {
				return nil, errors.Cause(err)
			}
			n.Op = promql.ItemSum

			// To aggregate count_values we simply sum(count_values(key, metric)) by (key)
		case promql.ItemCountValues:

			// First we must fetch the data into a vectorselector
			if s.Interval > 0 {
				result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
					Start: s.Start.Add(-offset - promql.LookbackDelta),
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

			ret := &promql.VectorSelector{Offset: offset}
			ret.SetSeries(series, promhttputil.WarningsConvert(warnings))

			// Replace with sum(count_values()) BY (label)
			return &promql.AggregateExpr{
				Op:       promql.ItemSum,
				Expr:     ret,
				Grouping: []string{n.Param.(*promql.StringLiteral).Val},
			}, nil

		case promql.ItemQuantile:
			// DO NOTHING
			// this caltulates an actual quantile over the resulting data
			// as such there is no way to reduce the load necessary here. If
			// the query is something like quantile(sum(foo)) then the inner aggregation
			// will reduce the required data

		// Both of these cases require some mechanism of knowing what labels to do the aggregation on.
		// WIthout that knowledge we require pulling all of the data in, so we do nothing
		case promql.ItemStddev:
			// DO NOTHING
		case promql.ItemStdvar:
			// DO NOTHING

		}

		if result != nil {
			iterators := promclient.IteratorsForValue(result)

			series := make([]storage.Series, len(iterators))
			for i, iterator := range iterators {
				series[i] = &proxyquerier.Series{iterator}
			}

			ret := &promql.VectorSelector{Offset: offset}
			ret.SetSeries(series, promhttputil.WarningsConvert(warnings))
			n.Expr = ret
			return n, nil
		}

	// Call is for things such as rate() etc. This can be sent directly to the
	// prometheus node to answer
	case *promql.Call:
		logrus.Debugf("call %v %v", n, n.Type())
		removeOffset()

		var result model.Value
		var warnings api.Warnings
		var err error
		if s.Interval > 0 {
			result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
				Start: s.Start.Add(-offset - promql.LookbackDelta),
				End:   s.End.Add(-offset),
				Step:  s.Interval,
			})
		} else {
			result, warnings, err = state.client.Query(ctx, n.String(), s.Start.Add(-offset))
		}

		if err != nil {
			return nil, errors.Cause(err)
		}

		// the "scalar()" function can return a scalar, if so we need to make sure we return
		// the correct type
		if scalarResult, ok := result.(*model.Scalar); ok {
			return &promql.NumberLiteral{float64(scalarResult.Value)}, nil
		}

		iterators := promclient.IteratorsForValue(result)
		series := make([]storage.Series, len(iterators))
		for i, iterator := range iterators {
			series[i] = &proxyquerier.Series{iterator}
		}

		ret := &promql.VectorSelector{Offset: offset}
		ret.SetSeries(series, promhttputil.WarningsConvert(warnings))
		return ret, nil

	// If we are simply fetching a Vector then we can fetch the data using the same step that
	// the query came in as (reducing the amount of data we need to fetch)
	case *promql.VectorSelector:
		// If the vector selector already has the data we can skip
		if n.HasSeries() {
			return nil, nil
		}

		var result model.Value
		var warnings api.Warnings
		var err error
		if s.Interval > 0 {
			result, warnings, err = state.client.QueryRange(ctx, n.String(), v1.Range{
				Start: s.Start.Add(-offset - promql.LookbackDelta),
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
		n.SetSeries(series, promhttputil.WarningsConvert(warnings))

	// If we hit this someone is asking for a matrix directly, if so then we don't
	// have anyway to ask for less-- since this is exactly what they are asking for
	case *promql.MatrixSelector:
		// DO NOTHING

	default:
		logrus.Debugf("default %v %s", n, reflect.TypeOf(n))

	}
	return nil, nil
}
