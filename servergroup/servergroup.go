package servergroup

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/relabel"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/promcache"
	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"

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
	Targets   []string
	apiClient promclient.API
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

func (s *ServerGroup) Cancel() {
	s.ctxCancel()
}

func (s *ServerGroup) Sync() {
	syncCh := s.targetManager.SyncCh()

	for targetGroupMap := range syncCh {
		targets := make([]string, 0)
		apiClients := make([]promclient.API, 0)
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
						Path:   s.Cfg.PathPrefix,
					}
					targets = append(targets, u.Host)

					client, err := api.NewClient(api.Config{Address: u.String(), RoundTripper: s.Client.Transport})
					if err != nil {
						panic(err) // TODO: shouldn't be possible? If this happens I guess we log and skip?
					}

					promAPIClient := v1.NewAPI(client)

					if s.Cfg.RemoteRead {
						u.Path = path.Join(u.Path, "api/v1/read")
						cfg := &remote.ClientConfig{
							URL: &config_util.URL{u},
							// TODO: from context?
							Timeout: model.Duration(time.Minute * 2),
						}
						remoteStorageClient, err := remote.NewClient(1, cfg)
						if err != nil {
							panic(err)
						}

						apiClients = append(apiClients, &promclient.PromAPIRemoteRead{promAPIClient, remoteStorageClient})
					} else {
						apiClients = append(apiClients, &promclient.PromAPIV1{promAPIClient})
					}

					// We remove all private labels after we set the target entry
					for name := range target {
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
							for k := range target {
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

		apiClientMetricFunc := func(i int, api, status string, took float64) {
			serverGroupSummary.WithLabelValues(targets[i], api, status).Observe(took)
		}

		var apiClient promclient.API
		apiClient = promclient.NewMultiAPI(apiClients, s.Cfg.GetAntiAffinity(), apiClientMetricFunc)
		if s.Cfg.CacheOptions != nil {
			d := s.Cfg.Digest()
			cacheClient, err := promcache.NewCacheClient(d[:], *s.Cfg.CacheOptions, apiClient)
			if err != nil {
				panic(err)
			}
			apiClient = cacheClient
		}

		s.state.Store(&ServerGroupState{
			Targets:   targets,
			apiClient: apiClient,
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

	// Copy/paste from upstream prometheus/common until https://github.com/prometheus/common/issues/144 is resolved
	tlsConfig, err := config_util.NewTLSConfig(&cfg.HTTPConfig.HTTPConfig.TLSConfig)
	if err != nil {
		return errors.Wrap(err, "error loading TLS client config")
	}
	// The only timeout we care about is the configured scrape timeout.
	// It is applied on request. So we leave out any timings here.
	var rt http.RoundTripper = &http.Transport{
		Proxy:               http.ProxyURL(cfg.HTTPConfig.HTTPConfig.ProxyURL.URL),
		MaxIdleConns:        20000,
		MaxIdleConnsPerHost: 1000, // see https://github.com/golang/go/issues/13801
		DisableKeepAlives:   false,
		TLSClientConfig:     tlsConfig,
		DisableCompression:  true,
		// 5 minutes is typically above the maximum sane scrape interval. So we can
		// use keepalive for all configurations.
		IdleConnTimeout: 5 * time.Minute,
		DialContext:     (&net.Dialer{Timeout: cfg.HTTPConfig.DialTimeout}).DialContext,
	}

	// If a bearer token is provided, create a round tripper that will set the
	// Authorization header correctly on each request.
	if len(cfg.HTTPConfig.HTTPConfig.BearerToken) > 0 {
		rt = config_util.NewBearerAuthRoundTripper(cfg.HTTPConfig.HTTPConfig.BearerToken, rt)
	} else if len(cfg.HTTPConfig.HTTPConfig.BearerTokenFile) > 0 {
		rt = config_util.NewBearerAuthFileRoundTripper(cfg.HTTPConfig.HTTPConfig.BearerTokenFile, rt)
	}

	if cfg.HTTPConfig.HTTPConfig.BasicAuth != nil {
		rt = config_util.NewBasicAuthRoundTripper(cfg.HTTPConfig.HTTPConfig.BasicAuth.Username, cfg.HTTPConfig.HTTPConfig.BasicAuth.Password, cfg.HTTPConfig.HTTPConfig.BasicAuth.PasswordFile, rt)
	}

	s.Client = &http.Client{Transport: rt}

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

// GetValue loads the raw data for a given set of matchers in the time range
func (s *ServerGroup) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	state := s.State()
	filteredMatchers, ok := FilterMatchers(state.Labels, matchers)
	if !ok {
		return nil, nil
	}

	val, err := state.apiClient.GetValue(ctx, start, end, filteredMatchers)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, state.Labels); err != nil {
		return nil, err
	}

	return val, nil
}

// Query performs a query for the given time.
func (s *ServerGroup) Query(ctx context.Context, query string, ts time.Time) (model.Value, error) {
	state := s.State()

	// Parse out the promql query into expressions etc.
	e, err := promql.ParseExpr(query)
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

	val, err := state.apiClient.Query(ctx, e.String(), ts)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, state.Labels); err != nil {
		return nil, err
	}
	return val, nil
}

// QueryRange performs a query for the given range.
func (s *ServerGroup) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, error) {
	state := s.State()

	// Parse out the promql query into expressions etc.
	e, err := promql.ParseExpr(query)
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

	val, err := state.apiClient.QueryRange(ctx, e.String(), r)
	if err != nil {
		return nil, err
	}
	if err := promhttputil.ValueAddLabelSet(val, state.Labels); err != nil {
		return nil, err
	}
	return val, nil
}

// LabelValues performs a query for the values of the given label.
func (s *ServerGroup) LabelValues(ctx context.Context, label string) (model.LabelValues, error) {
	state := s.State()

	val, err := state.apiClient.LabelValues(ctx, label)
	if err != nil {
		return nil, err
	}
	// do we have labels that match in our state
	if value, ok := state.Labels[model.LabelName(label)]; ok {
		return promclient.MergeLabelValues(val, model.LabelValues{value}), nil
	}
	return val, nil
}

// Series finds series by label matchers.
func (s *ServerGroup) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, error) {
	state := s.State()

	// Now we need to filter the matches sent to us for the labels associated with this
	// servergroup
	filteredMatches := make([]string, 0, len(matches))
	for _, matcher := range matches {
		// Parse out the promql query into expressions etc.
		e, err := promql.ParseExpr(matcher)
		if err != nil {
			return nil, err
		}

		// Walk the expression, to filter out any LabelMatchers that match etc.
		filterVisitor := &LabelFilterVisitor{s, state.Labels, true}
		if _, err := promql.Walk(ctx, filterVisitor, &promql.EvalStmt{Expr: e}, e, nil, nil); err != nil {
			return nil, err
		}
		// If we didn't match, lets skip
		if !filterVisitor.filterMatch {
			continue
		}
		// if we did match, lets assign the filtered version of the matcher
		filteredMatches = append(filteredMatches, e.String())
	}

	// If no matchers remain, then we don't have anything -- so skip
	if len(filteredMatches) == 0 {
		return nil, nil
	}

	v, err := state.apiClient.Series(ctx, filteredMatches, startTime, endTime)
	if err != nil {
		return nil, err
	}

	// add our state's labels to the labelsets we return
	for _, lset := range v {
		for k, v := range state.Labels {
			lset[k] = v
		}
	}
	return v, nil
}
