package proxystorage

import (
	"context"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"reflect"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/proxyquerier"
	"github.com/jacksontj/promxy/servergroup"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage/local"
	"github.com/prometheus/prometheus/storage/metric"
	promhttputil "github.com/prometheus/prometheus/util/httputil"
	"github.com/sirupsen/logrus"
)

type proxyStorageState struct {
	serverGroups []*servergroup.ServerGroup
	client       *http.Client

	q local.Querier
}

func (p *proxyStorageState) Ready() {
	for _, sg := range p.serverGroups {
		<-sg.Ready
	}
}

func (p *proxyStorageState) Cancel() {
	if p.serverGroups != nil {
		for _, sg := range p.serverGroups {
			sg.Cancel()
		}
	}
}

func NewProxyStorage() (*ProxyStorage, error) {
	return &ProxyStorage{}, nil
}

// TODO: rename?
type ProxyStorage struct {
	state atomic.Value
}

func (p *ProxyStorage) GetState() *proxyStorageState {
	tmp := p.state.Load()
	if sg, ok := tmp.(*proxyStorageState); ok {
		return sg
	} else {
		return nil
	}
}

func (p *ProxyStorage) ApplyConfig(c *proxyconfig.Config) error {
	failed := false

	newState := &proxyStorageState{
		serverGroups: make([]*servergroup.ServerGroup, len(c.ServerGroups)),
	}
	for i, sgCfg := range c.ServerGroups {
		tmp := servergroup.New()
		if err := tmp.ApplyConfig(sgCfg); err != nil {
			failed = true
			logrus.Errorf("Error applying config to server group: %s", err)
		}
		newState.serverGroups[i] = tmp
	}

	// stage the client swap as well
	client, err := promhttputil.NewClientFromConfig(c.PromxyConfig.HTTPConfig)
	if err != nil {
		failed = true
		logrus.Errorf("Unable to load client from config: %s", err)
	}

	// TODO: remove after fixes to upstream
	// Override the dial timeout
	transport := client.Transport.(*http.Transport)
	transport.DialContext = (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext

	newState.client = client

	newState.q = &proxyquerier.ProxyQuerier{
		newState.serverGroups,
		newState.client,
	}

	if failed {
		for _, sg := range newState.serverGroups {
			sg.Cancel()
		}
		return fmt.Errorf("Error Applying Config to one or more server group(s)")
	}

	newState.Ready()         // Wait for the newstate to be ready
	oldState := p.GetState() // Fetch the old state
	p.state.Store(newState)  // Store the new state
	if oldState != nil {
		oldState.Cancel() // Cancel the old one
	}

	return nil
}

// Handler to proxy requests to *a* server in serverGroups
func (p *ProxyStorage) ProxyHandler(w http.ResponseWriter, r *http.Request) {
	state := p.GetState()

	serverGroup := state.serverGroups[rand.Int()%len(state.serverGroups)]
	servers := serverGroup.Targets()
	if len(servers) <= 0 {
		http.Error(w, "no servers available in serverGroup", http.StatusBadGateway)
	}
	server := servers[rand.Int()%len(servers)]
	// TODO: failover
	parsedUrl, _ := url.Parse(server)

	proxy := httputil.NewSingleHostReverseProxy(parsedUrl)
	proxy.Transport = state.client.Transport

	proxy.ServeHTTP(w, r)
}

func (p *ProxyStorage) Querier() (local.Querier, error) {
	state := p.GetState()
	return state.q, nil
}

// This replaces promql Nodes with more efficient-to-fetch ones. This works by taking lower-layer
// chunks of the query, farming them out to prometheus hosts, then stitching the results back together.
// An example would be a sum, we can sum multiple sums and come up with the same result -- so we do.
// There are a few ground rules for this:
//      - Children cannot be AggregateExpr: aggregates have their own combining logic, so its not safe to send a subquery with additional aggregations
//      - offsets within the subtree must match: if they don't then we'll get mismatched data, so we wait until we are far enough down the tree that they converge
//      - Don't reduce accuracy/granularity: the intention of this is to get the correct data faster, meaning correctness overrules speed.
func (p *ProxyStorage) NodeReplacer(ctx context.Context, s *promql.EvalStmt, node promql.Node) (promql.Node, error) {
	// If there is a child that is an aggregator we cannot do anything (as they have their own
	// rules around combining). We'll skip this node and let a lower layer take this on
	aggFinder := &BooleanFinder{Func: func(node promql.Node) bool {
		_, ok := node.(*promql.AggregateExpr)
		return ok
	}}

	if _, err := promql.Walk(ctx, aggFinder, s, node, nil); err != nil {
		return nil, err
	}

	if aggFinder.Found {
		return nil, nil
	}

	// If the tree below us is not all the same offset, then we can't do anything below -- we'll need
	// to wait until further in execution where they all match
	var offset time.Duration

	visitor := &OffsetFinder{}
	if _, err := promql.Walk(ctx, visitor, s, node, nil); err != nil {
		return nil, err
	}
	// If we couldn't find an offset, then something is wrong-- lets skip
	// Also if there was an error, skip
	if !visitor.Found || visitor.Error != nil {
		return nil, nil
	}
	offset = visitor.Offset

	// Function to recursivelt remove offset. This is needed as we're using
	// the node API to String() the query to downstreams. Promql's iterators require
	// that the time be the abolute time, wheras the API returns them based on the
	// range you ask for (with the offset being implicit)
	// TODO: rename
	removeOffset := func() error {
		_, err := promql.Walk(ctx, &OffsetRemover{}, s, node, nil)
		return err
	}

	state := p.GetState()
	serverGroups := servergroup.ServerGroups(state.serverGroups)
	switch n := node.(type) {
	// Some AggregateExprs can be composed (meaning they are "reentrant". If the aggregation op
	// is reentrant/composable then we'll do so, otherwise we let it fall through to normal query mechanisms
	case *promql.AggregateExpr:
		logrus.Debugf("AggregateExpr %v", n)

		var result model.Value
		var err error

		// Not all Aggregation functions are composable, so we'll do what we can
		switch n.Op.String() {
		// All "reentrant" cases (meaning they can be done repeatedly and the outcome doesn't change)
		case "sum", "min", "max", "topk", "bottomk":
			var urlBase string
			values := url.Values{}
			removeOffset()
			values.Add("query", n.String())

			if s.Interval > 0 {
				values.Add("start", s.Start.Add(-offset-promql.StalenessDelta).String())
				values.Add("end", s.End.Add(-offset).String())
				values.Add("step", strconv.FormatFloat(s.Interval.Seconds(), 'f', -1, 64))
				urlBase = "/api/v1/query_range"
			} else {
				values.Add("time", s.Start.Add(-offset).String())
				urlBase = "/api/v1/query"
			}

			result, err = serverGroups.GetData(ctx, urlBase, values, state.client)
			if err != nil {
				return nil, err
			}

		// Convert avg into sum() / count()
		case "avg":
			// Replace with sum() / count()
			return &promql.BinaryExpr{
				Op: 24, // Divide  TODO
				LHS: &promql.AggregateExpr{
					Op:               41, // sum() TODO
					Expr:             n.Expr,
					Param:            n.Param,
					Grouping:         n.Grouping,
					Without:          n.Without,
					KeepCommonLabels: n.KeepCommonLabels,
				},

				RHS: &promql.AggregateExpr{
					Op:               40, // count() TODO
					Expr:             n.Expr,
					Param:            n.Param,
					Grouping:         n.Grouping,
					Without:          n.Without,
					KeepCommonLabels: n.KeepCommonLabels,
				},
				VectorMatching: &promql.VectorMatching{Card: promql.CardOneToOne},
			}, nil

		// For count we simply need to change this to a sum over the data we get back
		case "count":
			var urlBase string
			values := url.Values{}
			removeOffset()
			values.Add("query", n.String())

			if s.Interval > 0 {
				values.Add("start", s.Start.Add(-offset-promql.StalenessDelta).String())
				values.Add("end", s.End.Add(-offset).String())
				values.Add("step", strconv.FormatFloat(s.Interval.Seconds(), 'f', -1, 64))
				urlBase = "/api/v1/query_range"
			} else {
				values.Add("time", s.Start.Add(-offset).String())
				urlBase = "/api/v1/query"
			}

			result, err = serverGroups.GetData(ctx, urlBase, values, state.client)
			if err != nil {
				return nil, err
			}
			// TODO: have a reverse method in promql/lex.go
			n.Op = 41 // SUM

		case "stddev": // TODO: something?
		case "stdvar": // TODO: something?

			// Do nothing, we want to allow the VectorSelector to fall through to do a query_range.
			// Unless we find another way to decompose the query, this is all we can do
		case "count_values":
			// DO NOTHING

		case "quantile": // TODO: something?

		}

		if result != nil {
			iterators := promclient.IteratorsForValue(result)
			returnIterators := make([]local.SeriesIterator, len(iterators))
			for i, item := range iterators {
				returnIterators[i] = item
			}

			ret := &promql.VectorSelector{Offset: offset}
			ret.SetIterators(returnIterators)
			n.Expr = ret
			return n, nil
		}

	// Call is for things such as rate() etc. This can be sent directly to the
	// prometheus node to answer
	case *promql.Call:
		logrus.Debugf("call %v %v", n, n.Type())
		var urlBase string
		values := url.Values{}
		removeOffset()
		values.Add("query", n.String())

		if s.Interval > 0 {
			values.Add("start", s.Start.Add(-offset-promql.StalenessDelta).String())
			values.Add("end", s.End.Add(-offset).String())
			values.Add("step", strconv.FormatFloat(s.Interval.Seconds(), 'f', -1, 64))
			urlBase = "/api/v1/query_range"
		} else {
			values.Add("time", s.Start.Add(-offset).String())
			urlBase = "/api/v1/query"
		}

		result, err := serverGroups.GetData(ctx, urlBase, values, state.client)
		if err != nil {
			return nil, err
		}
		iterators := promclient.IteratorsForValue(result)
		returnIterators := make([]local.SeriesIterator, len(iterators))
		for i, item := range iterators {
			returnIterators[i] = item
		}

		ret := &promql.VectorSelector{Offset: offset}
		ret.SetIterators(returnIterators)
		return ret, nil

		// If we are simply fetching a Vector then we can fetch the data using the same step that
		// the query came in as (reducing the amount of data we need to fetch)
	// If we are simply fetching data, lets do that
	case *promql.VectorSelector:
		// Don't attempt to fetch internally replaced things
		if n.HasIterators() {
			return nil, nil
		}
		logrus.Debugf("vectorSelector %v", n)
		var urlBase string
		values := url.Values{}
		removeOffset()
		values.Add("query", n.String())
		n.Offset = offset
		if s.Interval > 0 {
			values.Add("start", s.Start.Add(-offset-promql.StalenessDelta).String())
			values.Add("end", s.End.Add(-offset).String())
			values.Add("step", strconv.FormatFloat(s.Interval.Seconds(), 'f', -1, 64))
			urlBase = "/api/v1/query_range"
		} else {
			values.Add("time", s.Start.Add(-offset).String())
			urlBase = "/api/v1/query"
		}

		result, err := serverGroups.GetData(ctx, urlBase, values, state.client)
		if err != nil {
			return nil, err
		}
		iterators := promclient.IteratorsForValue(result)
		returnIterators := make([]local.SeriesIterator, len(iterators))
		for i, item := range iterators {
			returnIterators[i] = item
		}

		n.SetIterators(returnIterators)
		return n, nil

	// If we hit this someone is asking for a matrix directly, if so then we don't
	// have anyway to ask for less-- since this is exactly what they are asking for
	case *promql.MatrixSelector:
		// DO NOTHING

	default:
		logrus.Debugf("default %v %s", n, reflect.TypeOf(n))

	}
	return nil, nil

}

// TODO: IMPLEMENT??

// Append appends a sample to the underlying storage. Depending on the
// storage implementation, there are different guarantees for the fate
// of the sample after Append has returned. Remote storage
// implementation will simply drop samples if they cannot keep up with
// sending samples. Local storage implementations will only drop metrics
// upon unrecoverable errors.
func (p *ProxyStorage) Append(*model.Sample) error { return nil }

// NeedsThrottling returns true if the underlying storage wishes to not
// receive any more samples. Append will still work but might lead to
// undue resource usage. It is recommended to call NeedsThrottling once
// before an upcoming batch of Append calls (e.g. a full scrape of a
// target or the evaluation of a rule group) and only proceed with the
// batch if NeedsThrottling returns false. In that way, the result of a
// scrape or of an evaluation of a rule group will always be appended
// completely or not at all, and the work of scraping or evaluation will
// not be performed in vain. Also, a call of NeedsThrottling is
// potentially expensive, so limiting the number of calls is reasonable.
//
// Only SampleAppenders for which it is considered critical to receive
// each and every sample should ever return true. SampleAppenders that
// tolerate not receiving all samples should always return false and
// instead drop samples as they see fit to avoid overload.
func (p *ProxyStorage) NeedsThrottling() bool { return false }

// Drop all time series associated with the given label matchers. Returns
// the number series that were dropped.
func (p *ProxyStorage) DropMetricsForLabelMatchers(context.Context, ...*metric.LabelMatcher) (int, error) {
	// TODO: implement
	return 0, nil
}

// Run the various maintenance loops in goroutines. Returns when the
// storage is ready to use. Keeps everything running in the background
// until Stop is called.
func (p *ProxyStorage) Start() error {
	return nil
}

// Stop shuts down the Storage gracefully, flushes all pending
// operations, stops all maintenance loops,and frees all resources.
func (p *ProxyStorage) Stop() error {
	return nil
}

// WaitForIndexing returns once all samples in the storage are
// indexed. Indexing is needed for FingerprintsForLabelMatchers and
// LabelValuesForLabelName and may lag behind.
func (p *ProxyStorage) WaitForIndexing() {}
