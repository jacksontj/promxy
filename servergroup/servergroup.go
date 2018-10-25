package servergroup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/client_golang/prometheus"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/relabel"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/sirupsen/logrus"

	sd_config "github.com/prometheus/prometheus/discovery/config"
)

var (
	// TODO: have a marker for "which" servergroup
	serverGroupSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "server_group_request_duration_seconds",
		Help: "Summary of calls to servergroup instances",
	}, []string{"host", "call", "status"})
)

func init() {
	prometheus.MustRegister(serverGroupSummary)
}

func New() *ServerGroup {
	ctx, ctxCancel := context.WithCancel(context.Background())
	// Create the targetSet (which will maintain all of the updating etc. in the background)
	sg := &ServerGroup{
		ctx:       ctx,
		ctxCancel: ctxCancel,
		Ready:     make(chan struct{}),
	}

	lvl := promlog.AllowedLevel{}
	if err := lvl.Set("info"); err != nil {
		panic(err)
	}
	sg.targetManager = discovery.NewManager(ctx, promlog.New(lvl))
	// Background the updating
	go sg.targetManager.Run()
	go sg.Sync()

	return sg

}

// Encapsulate the state of a serverGroup from service discovery
type ServerGroupState struct {
	// Targets is the list of target URLs for this discovery round
	Targets []string
	// Labels that should be applied to metrics from this serverGroup
	Labels model.LabelSet
}

type ServerGroup struct {
	ctx       context.Context
	ctxCancel context.CancelFunc

	loaded bool
	Ready  chan struct{}

	// TODO: lock/atomics on cfg and client
	Cfg           *Config
	Client        *http.Client
	targetManager *discovery.Manager

	OriginalURLs []string

	state atomic.Value
}

func (s *ServerGroup) getPath(p string) string {
	if s.Cfg.PathPrefix != "" {
		return path.Join(s.Cfg.PathPrefix, p)
	}
	return p
}

func (s *ServerGroup) Cancel() {
	s.ctxCancel()
}

func (s *ServerGroup) Sync() {
	syncCh := s.targetManager.SyncCh()

	for targetGroupMap := range syncCh {
		targets := make([]string, 0)
		var ls model.LabelSet

		for _, targetGroupList := range targetGroupMap {
			for _, targetGroup := range targetGroupList {
				for _, target := range targetGroup.Targets {

					target = relabel.Process(target, s.Cfg.RelabelConfigs...)
					// Check if the target was dropped, if so we skip it
					if target == nil {
						continue
					}

					u := &url.URL{
						Scheme: string(s.Cfg.GetScheme()),
						Host:   string(target[model.AddressLabel]),
					}
					targets = append(targets, u.String())

					// We remove all private labels after we set the target entry
					for name, _ := range target {
						if strings.HasPrefix(string(name), model.ReservedLabelPrefix) {
							delete(target, name)
						}
					}

					// Now we want to generate the labelset for this servergroup based
					// on the relabel_config. In the event that the relabel_config returns
					// different labelsets per-host we'll take the intersection. This is
					// important as these labels will be added to each result from these
					// targets, and since they are in the same targetgroup they are supposed
					// to be the same-- so if they had different labels we'd be duplicating
					// the metrics, which we don't want.
					if ls == nil {
						ls = target
					} else {
						if !ls.Equal(target) {
							// If not equal, we want ls to be the intersection of all the labelsets
							// we see, this is because targetManager.SyncCh() has no error reporting
							// mechanism
							changedKeys := make([]model.LabelName, 0)
							for k, v := range ls {
								// if the new labelset doesn't have it, remove it
								if oV, ok := target[k]; !ok {
									delete(ls, k)
									changedKeys = append(changedKeys, k)
								} else if v != oV { // If the keys both exist but values don't
									delete(ls, k)
									delete(target, k)
									changedKeys = append(changedKeys, k)
								}
							}
							for k, _ := range target {
								// if the new labelset doesn't have it, remove it
								if _, ok := ls[k]; !ok {
									delete(target, k)
									changedKeys = append(changedKeys, k)
								}
							}
							logrus.Warnf("relabel_configs for server group created different labelsets for targets within the same server group; using intersection of labelsets: different keys=%v", changedKeys)
						}
					}
				}
			}
		}

		s.state.Store(&ServerGroupState{
			Targets: targets,
			// Merge labels we just got with the statically configured ones, this way the
			// static ones take priority
			Labels: ls.Merge(s.Cfg.Labels),
		})

		if !s.loaded {
			s.loaded = true
			close(s.Ready)
		}
	}
}

// TODO: move config + client into state object to be swapped with atomics
func (s *ServerGroup) ApplyConfig(cfg *Config) error {
	s.Cfg = cfg

	// stage the client swap as well
	// TODO: better name
	client, err := config_util.NewClientFromConfig(cfg.HTTPConfig, "somename")
	if err != nil {
		return fmt.Errorf("Unable to load client from config: %s", err)
	}

	// TODO
	// as of now the service_discovery mechanisms in prometheus have no mechanism of
	// removing unhealthy hosts (through relableing or otherwise). So for now we simply
	// set a dial timeout, assuming that if we can't TCP connect in 200ms it is probably
	// dead. Our options for doing this better in the future are (1) configurable
	// dial timeout (2) healthchecks (3) track "healthiness" of downstream based on our
	// requests to it -- not through other healthchecks
	// Override the dial timeout
	switch transport := client.Transport.(type) {
	case *http.Transport:
		transport.DialContext = (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext
		// TODO: basic auth?  as reported in #70 basicAuth doesn't use this timeout.
		// This is because prometheus has its own RoundTripper (*config.basicAuthRoundTripper)
		// which doesn't set a timeout or expose a way to do so. For now I'm changing
		// this just so it won't panic, but this is not a long-term solution to the issue.
	}

	s.Client = client

	if err := s.targetManager.ApplyConfig(map[string]sd_config.ServiceDiscoveryConfig{"foo": cfg.Hosts}); err != nil {
		return err
	}
	return nil
}

func (s *ServerGroup) State() *ServerGroupState {
	tmp := s.state.Load()
	if ret, ok := tmp.(*ServerGroupState); ok {
		return ret
	} else {
		return nil
	}
}

func (s *ServerGroup) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	state := s.State()
	filteredMatchers, ok := FilterMatchers(state.Labels, matchers)
	if !ok {
		return nil, nil
	}

	if s.Cfg.RemoteRead {
		return s.RemoteRead(ctx, start, end, filteredMatchers)
	} else {
		// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
		pql, err := promhttputil.MatcherToString(filteredMatchers)
		if err != nil {
			return nil, err
		}

		// Create the query params
		values := url.Values{}

		// We want to do a normal query (for raw data)
		urlBase := "/api/v1/query"

		// We want to grab only the raw datapoints, so we do that through the query interface
		// passing in a duration that is at least as long as ours (the added second is to deal
		// with any rounding error etc since the duration is a floating point and we are casting
		// to an int64
		values.Add("query", pql+fmt.Sprintf("[%ds]", int64(end.Sub(start).Seconds())+1))
		values.Add("time", model.Time(timestamp.FromTime(end)).String())

		return s.GetData(ctx, urlBase, values)
	}
}

func (s *ServerGroup) RemoteRead(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	state := s.State()

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(state.Targets))

	path := s.getPath("/api/v1/read")

	for i, target := range state.Targets {
		resultChans[i] = make(chan interface{}, 1)
		parsedUrl, err := url.Parse(target + path)
		if err != nil {
			return nil, err
		}
		go func(retChan chan interface{}, stringUrl *url.URL) {
			cfg := &remote.ClientConfig{
				URL: &config_util.URL{parsedUrl},
				// TODO: from context?
				Timeout: model.Duration(time.Minute * 2),
			}
			client, err := remote.NewClient(1, cfg)
			if err != nil {
				retChan <- err
				return
			}

			start := time.Now()
			query, err := remote.ToQuery(int64(timestamp.FromTime(start)), int64(timestamp.FromTime(end)), matchers, nil)
			if err != nil {
				retChan <- err
				return
			}
			// http://localhost:8083/api/v1/query?query=%7B__name__%3D%22metric%22%7D%5B302s%5D&time=21
			result, err := client.Read(childContext, query)
			took := time.Now().Sub(start)

			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "remoteread", "error").Observe(took.Seconds())
				retChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "remoteread", "success").Observe(took.Seconds())
				// convert result (timeseries) to SampleStream
				matrix := make(model.Matrix, len(result.Timeseries))
				for i, ts := range result.Timeseries {
					metric := make(model.Metric)
					for _, label := range ts.Labels {
						metric[model.LabelName(label.Name)] = model.LabelValue(label.Value)
					}

					samples := make([]model.SamplePair, len(ts.Samples))
					for x, sample := range ts.Samples {
						samples[x] = model.SamplePair{
							Timestamp: model.Time(sample.Timestamp),
							Value:     model.SampleValue(sample.Value),
						}
					}

					matrix[i] = &model.SampleStream{
						Metric: metric,
						Values: samples,
					}
				}

				err = promhttputil.ValueAddLabelSet(matrix, state.Labels)
				if err != nil {
					retChan <- err
					return
				}

				retChan <- matrix
			}

		}(resultChans[i], parsedUrl)
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(state.Targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				// If the server responded with a non-success, lets mark that as an error
				// TODO: check qData.ResultType
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(s.Cfg.GetAntiAffinity(), result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(state.Targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

// TODO: change the args here from url.Values to something else (probably matchers and start/end more like remote read)
func (s *ServerGroup) GetData(ctx context.Context, path string, inValues url.Values) (model.Value, error) {
	state := s.State()

	path = s.getPath(path)
	// Make a copy of Values since we're going to mutate it
	values := make(url.Values)
	for k, v := range inValues {
		values[k] = v
	}

	// Parse out the promql query into expressions etc.
	e, err := promql.ParseExpr(values.Get("query"))
	if err != nil {
		return nil, err
	}

	// Walk the expression, to filter out any LabelMatchers that match etc.
	filterVisitor := &LabelFilterVisitor{s, state.Labels, true}
	if _, err := promql.Walk(ctx, filterVisitor, &promql.EvalStmt{Expr: e}, e, nil, nil); err != nil {
		return nil, err
	}
	if !filterVisitor.filterMatch {
		return nil, nil
	}
	values.Set("query", e.String())

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(state.Targets))

	for i, target := range state.Targets {
		resultChans[i] = make(chan interface{}, 1)
		parsedUrl, err := url.Parse(target + path)
		if err != nil {
			return nil, err
		}
		parsedUrl.RawQuery = values.Encode()
		go func(retChan chan interface{}, stringUrl string) {
			start := time.Now()
			result, err := promclient.GetData(childContext, stringUrl, s.Client, state.Labels)
			took := time.Now().Sub(start)
			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "error").Observe(took.Seconds())
				retChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "success").Observe(took.Seconds())
				retChan <- result
			}
		}(resultChans[i], parsedUrl.String())
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(state.Targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				// TODO: check qData.ResultType
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(s.Cfg.GetAntiAffinity(), result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(state.Targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

func (s *ServerGroup) GetValuesForLabelName(ctx context.Context, path string) ([]model.LabelValue, error) {
	state := s.State()
	path = s.getPath(path)

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(state.Targets))

	for i, target := range state.Targets {
		resultChans[i] = make(chan interface{}, 1)
		parsedUrl, err := url.Parse(target + path)
		if err != nil {
			return nil, err
		}
		go func(retChan chan interface{}, stringUrl string) {
			start := time.Now()
			result, err := promclient.GetValuesForLabelName(childContext, stringUrl, s.Client)
			took := time.Now().Sub(start)
			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "label_values", "error").Observe(took.Seconds())
				retChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "label_values", "success").Observe(took.Seconds())
				retChan <- result
			}
		}(resultChans[i], parsedUrl.String())
	}

	// Wait for results as we get them
	var result []model.LabelValue
	var lastError error
	errCount := 0
	for i := 0; i < len(state.Targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case []model.LabelValue:
				if result == nil {
					result = retTyped
				} else {
					result = promclient.MergeLabelValues(result, retTyped)
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(state.Targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

func (s *ServerGroup) GetSeries(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	state := s.State()

	filteredMatchers, ok := FilterMatchers(state.Labels, matchers)
	if !ok {
		return nil, nil
	}

	// Create the query params
	values := url.Values{}

	urlBase := s.getPath("/api/v1/series")

	// Add matchers
	// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
	pql, err := promhttputil.MatcherToString(filteredMatchers)
	if err != nil {
		return nil, err
	}
	values.Add("match[]", pql)

	values.Add("start", model.Time(timestamp.FromTime(start)).String())
	values.Add("end", model.Time(timestamp.FromTime(end)).String())

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChans := make([]chan interface{}, len(state.Targets))

	for i, target := range state.Targets {
		resultChans[i] = make(chan interface{}, 1)
		parsedUrl, err := url.Parse(target + urlBase)
		if err != nil {
			return nil, err
		}
		parsedUrl.RawQuery = values.Encode()
		go func(retChan chan interface{}, stringUrl string) {
			start := time.Now()
			result, err := promclient.GetSeries(childContext, stringUrl, s.Client, state.Labels)
			took := time.Now().Sub(start)
			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "error").Observe(took.Seconds())
				retChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "success").Observe(took.Seconds())
				retChan <- result
			}
		}(resultChans[i], parsedUrl.String())
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(state.Targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case ret := <-resultChans[i]:
			switch retTyped := ret.(type) {
			case error:
				lastError = retTyped
				errCount++
			case model.Value:
				// TODO: check qData.ResultType
				if result == nil {
					result = retTyped
				} else {
					var err error
					result, err = promhttputil.MergeValues(s.Cfg.GetAntiAffinity(), result, retTyped)
					if err != nil {
						return nil, err
					}
				}
			default:
				return nil, fmt.Errorf("Unknown return type")
			}
		}
	}

	if errCount != 0 && errCount == len(state.Targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}
