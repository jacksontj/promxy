package servergroup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	prom_config "github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/sigv4"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/logging"
	"github.com/jacksontj/promxy/pkg/middleware"
	"github.com/jacksontj/promxy/pkg/promclient"
	//	sd_config "github.com/prometheus/prometheus/discovery/config"
)

// DiscoveryUpdateInterval controls how often each server group's discovery
// manager coalesces and applies target updates. Prometheus defaults this to 5s
// to throttle chatty service-discovery mechanisms; promxy keeps that default so
// production behavior is unchanged. It is exposed as a var mainly so tests can
// drive it low — the discovery manager only emits its first target set (and
// thus lets the server group become Ready) on the first tick of this interval,
// so a 5s value adds ~5s of startup latency to every proxy the tests spin up.
// Must be > 0 (time.NewTicker panics on zero).
var DiscoveryUpdateInterval = 5 * time.Second

var (
	// TODO: have a marker for "which" servergroup
	serverGroupSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "server_group_request_duration_seconds",
		Help: "Summary of calls to servergroup instances",
	}, []string{"host", "call", "status"})

	// serverGroupTargets tracks the number of targets currently discovered for
	// each server group, so a zero-target group can be alerted on (e.g.
	// `server_group_targets == 0`). The ordinal is guaranteed unique; the name
	// label is the optional, human-readable group name (empty when unset).
	serverGroupTargets = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "server_group_targets",
		Help: "Number of targets currently discovered for a server group.",
	}, []string{"ordinal", "name"})
)

func init() {
	prometheus.MustRegister(serverGroupSummary)
	prometheus.MustRegister(serverGroupTargets)
}

// DefaultRemoteReadTimeout bounds a remote_read request for a server group that
// sets no explicit timeout.
const DefaultRemoteReadTimeout = 2 * time.Minute

// newRemoteReadConfig builds the remote_read client config for a server group.
//
// Note that remote.ClientConfig.Timeout becomes a deadline on the whole read,
// while cfg.Timeout bounds only the response headers on the HTTP path (it is
// wired to Transport.ResponseHeaderTimeout). Reusing the knob here is therefore
// a stricter bound than it is there, which is deliberate: remote_read carries
// the largest payloads, so it is the path where a stuck request costs the most.
func newRemoteReadConfig(cfg *Config, u *url.URL) *remote.ClientConfig {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultRemoteReadTimeout
	}
	return &remote.ClientConfig{
		URL:              &config_util.URL{URL: u},
		HTTPClientConfig: cfg.HTTPConfig.HTTPConfig,
		SigV4Config:      cfg.HTTPConfig.SigV4Config,
		Timeout:          model.Duration(timeout),
		ChunkedReadLimit: prom_config.DefaultChunkedReadLimit,
	}
}

// New creates a new servergroup
func NewServerGroup() (*ServerGroup, error) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	// Create the targetSet (which will maintain all of the updating etc. in the background)
	sg := &ServerGroup{
		ctx:       ctx,
		ctxCancel: ctxCancel,
		Ready:     make(chan struct{}),
	}

	// TODO: route SD metrics into the global registry. We use a fresh registry
	// here to avoid double-registration when multiple servergroups exist.
	sdMetrics, err := discovery.RegisterSDMetrics(prometheus.NewRegistry(), discovery.NewRefreshMetrics(prometheus.NewRegistry()))
	if err != nil {
		return nil, err
	}
	sdLogger := logging.NewLogger(logrus.WithField("component", "servergroup-discovery"))
	sg.targetManager = discovery.NewManager(ctx, sdLogger, prometheus.NewRegistry(), sdMetrics, discovery.Updatert(DiscoveryUpdateInterval))
	if sg.targetManager == nil {
		return nil, fmt.Errorf("failed to create discovery manager")
	}
	// Background the updating
	go sg.targetManager.Run()
	go sg.Sync()

	return sg, nil

}

// ServerGroupState encapsulates the state of a serverGroup from service discovery
type ServerGroupState struct {
	// Targets is the list of target URLs for this discovery round
	Targets   []string
	apiClient promclient.API

	ctx       context.Context
	ctxCancel context.CancelFunc
}

// ServerGroup encapsulates a set of prometheus downstreams to query/aggregate
type ServerGroup struct {
	ctx       context.Context
	ctxCancel context.CancelFunc

	loaded bool
	Ready  chan struct{}

	// cfg and client are published by ApplyConfig and read concurrently by
	// RoundTrip and by the Sync goroutine (which is started before the first
	// ApplyConfig ever runs), so both are held as atomic pointers. Readers must
	// take a single snapshot per operation (via Config()/httpClient()) rather
	// than re-loading per field, or they can straddle two configurations.
	cfg           atomic.Pointer[Config]
	client        atomic.Pointer[http.Client]
	targetManager *discovery.Manager

	OriginalURLs []string

	state atomic.Value

	// histogramCache backs IsHistogramMetric. The background refresh loop is
	// only started when the config's native_histogram metadata refresh interval
	// is > 0; when zero, the cache stays empty and IsHistogramMetric always
	// returns false (AST-only routing). See pkg/servergroup/histogram_cache.go.
	histogramCache histogramMetadataCache
}

// IsHistogramMetric reports whether the given metric name is known to be a
// histogram metric in this server group's most recent metadata snapshot.
// Returns false when the metadata cache is disabled (the default), when no
// fetch has succeeded yet, or when the name simply isn't a histogram.
//
// Used by the proxy-level histogram routing decision to disable HTTP-API
// pushdown for queries that touch histogram metrics, even when the query
// doesn't invoke one of the histogram-only PromQL functions.
func (s *ServerGroup) IsHistogramMetric(name string) bool {
	return s.histogramCache.Contains(name)
}

// Config returns the configuration most recently published by ApplyConfig. It
// returns nil if ApplyConfig has not (successfully) run yet, so callers must
// nil-check the result. The returned *Config must be treated as read-only; take
// a single snapshot per operation instead of calling this repeatedly.
func (s *ServerGroup) Config() *Config {
	return s.cfg.Load()
}

// httpClient returns the *http.Client most recently published by ApplyConfig,
// or nil if ApplyConfig has not (successfully) run yet.
func (s *ServerGroup) httpClient() *http.Client {
	return s.client.Load()
}

// groupIdentifier returns a human-readable identifier for this server group for
// use in logs.
func (s *ServerGroup) groupIdentifier() string {
	return configIdentifier(s.Config())
}

// configIdentifier returns a human-readable identifier for the given server
// group config for use in logs. The ordinal is always included (it is
// guaranteed unique); the optional, non-unique name is appended when set.
func configIdentifier(cfg *Config) string {
	if cfg == nil {
		return "unknown"
	}
	if cfg.Name != "" {
		return fmt.Sprintf("ord=%d name=%s", cfg.Ordinal, cfg.Name)
	}
	return fmt.Sprintf("ord=%d", cfg.Ordinal)
}

func (s *ServerGroup) logTargetTransition(cfg *Config, oldCount, newCount int, initial bool) {
	fields := logrus.Fields{
		"old_targets": oldCount,
		"new_targets": newCount,
	}
	if cfg != nil {
		fields["ordinal"] = cfg.Ordinal
		if cfg.Name != "" {
			fields["name"] = cfg.Name
		}
	}

	ident := configIdentifier(cfg)
	switch {
	case initial && newCount == 0:
		logrus.WithFields(fields).Warnf("ServerGroup %s started with zero targets; check service discovery configuration and relabel rules", ident)
	case oldCount > 0 && newCount == 0:
		logrus.WithFields(fields).Warnf("ServerGroup %s transitioned to zero targets; check service discovery configuration and relabel rules", ident)
	}
}

// Cancel stops backround processes (e.g. discovery manager)
func (s *ServerGroup) Cancel() {
	s.ctxCancel()
}

// RoundTrip allows us to intercept and mutate downstream HTTP requests at the transport level
func (s *ServerGroup) RoundTrip(r *http.Request) (*http.Response, error) {
	// Snapshot the config and client once for this request; ApplyConfig may
	// swap either one concurrently.
	cfg := s.Config()
	client := s.httpClient()
	if client == nil {
		return nil, fmt.Errorf("servergroup %s has no client; no configuration applied yet", configIdentifier(cfg))
	}

	for k, v := range middleware.GetHeaders(r.Context()) {
		r.Header.Set(k, v)
	}
	if cfg != nil {
		for k, v := range cfg.HTTPClientHeaders {
			r.Header.Set(k, v)
			logrus.Tracef("Set ServerGroup custom header %s: %s", k, v)
		}
	}
	// Ensure Body is non-nil so downstream transports (e.g. SigV4) that
	// unconditionally read the body don't panic on GET requests.
	if r.Body == nil {
		r.Body = http.NoBody
	}
	return client.Transport.RoundTrip(r)
}

// Sync updates the targets from our discovery manager
func (s *ServerGroup) Sync() {
	syncCh := s.targetManager.SyncCh()

	for {
		select {
		case <-s.ctx.Done():
			return
		case targetGroupMap := <-syncCh:
			logrus.Debug("Updating targets from discovery manager")
			// TODO: retry and error handling
			err := s.loadTargetGroupMap(targetGroupMap)
			for err != nil {
				logrus.Errorf("Error loading servergroup, retrying: %v", err)
				// TODO: configurable backoff
				select {
				case <-time.After(time.Second):
					err = s.loadTargetGroupMap(targetGroupMap)
				case <-s.ctx.Done():
					return
				}
			}
		}
	}
}

func (s *ServerGroup) loadTargetGroupMap(targetGroupMap map[string][]*targetgroup.Group) (err error) {
	// Snapshot the config once for the whole load; ApplyConfig may publish a
	// new one while we're building clients and we must not straddle two.
	cfg := s.Config()
	if cfg == nil {
		return fmt.Errorf("no configuration applied to servergroup yet")
	}

	targets := make([]string, 0)
	apiClients := make([]promclient.API, 0)

	ctx, ctxCancel := context.WithCancel(context.Background())
	oldState := s.State()
	oldCount := 0
	if oldState != nil {
		oldCount = len(oldState.Targets)
	}
	defer func() {
		if err != nil {
			ctxCancel()
		}
	}()

	for _, targetGroupList := range targetGroupMap {
		for _, targetGroup := range targetGroupList {
			for _, target := range targetGroup.Targets {

				lbls := make([]labels.Label, 0, len(target)+len(targetGroup.Labels)+2)

				for ln, lv := range target {
					lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
				}

				for ln, lv := range targetGroup.Labels {
					if _, ok := target[ln]; !ok {
						lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
					}
				}

				lbls = append(lbls, labels.Label{Name: model.SchemeLabel, Value: string(cfg.Scheme)})
				lbls = append(lbls, labels.Label{Name: PathPrefixLabel, Value: string(cfg.PathPrefix)})

				lset := labels.New(lbls...)

				logrus.Tracef("Potential target pre-relabel: %v", lset)
				lset, keep := relabel.Process(lset, cfg.RelabelConfigs...)
				logrus.Tracef("Potential target post-relabel: %v", lset)
				// Check if the target was dropped, if so we skip it
				if !keep || lset.IsEmpty() {
					continue
				}

				// If there is no address, then we can't use this set of targets
				if v := lset.Get(model.AddressLabel); v == "" {
					return fmt.Errorf("discovery target is missing address label: %v", lset)
				}

				u := &url.URL{
					Scheme: lset.Get(model.SchemeLabel),
					Host:   lset.Get(model.AddressLabel),
					Path:   lset.Get(PathPrefixLabel),
				}

				targets = append(targets, u.Host)

				client, err := api.NewClient(api.Config{Address: u.String(), RoundTripper: s})
				if err != nil {
					return err
				}

				if len(cfg.QueryParams) > 0 {
					client = promclient.NewClientArgsWrap(client, cfg.QueryParams)
				}

				var apiClient promclient.API
				apiClient = &promclient.PromAPIV1{API: v1.NewAPI(client), Client: client}

				// If debug logging is enabled, wrap the client with a debugAPI client
				// Since these are called in the reverse order of what we add, we want
				// to make sure that this is the first wrap of the client
				if logrus.GetLevel() >= logrus.DebugLevel {
					apiClient = &promclient.DebugAPI{apiClient, u.String()}
				}

				if cfg.RemoteRead {
					u.Path = path.Join(u.Path, cfg.RemoteReadPath)
					remoteStorageClient, err := remote.NewReadClient("foo", newRemoteReadConfig(cfg, u))
					if err != nil {
						return err
					}

					apiClient = &promclient.PromAPIRemoteRead{apiClient, remoteStorageClient}
				}

				// Optionally add time range layers
				if cfg.AbsoluteTimeRangeConfig != nil {
					apiClient = &promclient.AbsoluteTimeFilter{
						API:      apiClient,
						Start:    cfg.AbsoluteTimeRangeConfig.Start,
						End:      cfg.AbsoluteTimeRangeConfig.End,
						Truncate: cfg.AbsoluteTimeRangeConfig.Truncate,
					}
				}

				if cfg.RelativeTimeRangeConfig != nil {
					apiClient = &promclient.RelativeTimeFilter{
						API:      apiClient,
						Start:    cfg.RelativeTimeRangeConfig.Start,
						End:      cfg.RelativeTimeRangeConfig.End,
						Truncate: cfg.RelativeTimeRangeConfig.Truncate,
					}
				}

				// Optionally re-stamp step-aligned query_range results back onto the
				// requested step grid. Enabled per-server-group for backends (e.g.
				// Mimir/Cortex) that snap query_range output to the epoch grid; see #787.
				if cfg.AlignQueryRangeWithStep {
					apiClient = &promclient.StepAlignClient{API: apiClient}
				}

				// We remove all private labels after we set the target entry
				modelLabelSet := make(model.LabelSet, lset.Len())
				lset.Range(func(lbl labels.Label) {
					if !strings.HasPrefix(string(lbl.Name), model.ReservedLabelPrefix) {
						modelLabelSet[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
					}
				})

				// Inject static matchers into all requests sent to this target. This is
				// applied beneath the label-manipulation wrappers below so the matchers
				// reach the downstream verbatim, without interacting with label_filter's
				// query filtering or metrics_relabel's matcher reversal.
				if injectMatchers, err := cfg.GetInjectMatchers(); err != nil {
					return err
				} else if len(injectMatchers) > 0 {
					apiClient, err = promclient.NewInjectMatchersClient(apiClient, injectMatchers)
					if err != nil {
						return err
					}
				}

				// Add labels
				apiClient = &promclient.AddLabelClient{apiClient, modelLabelSet.Merge(cfg.Labels)}

				// Add MetricRelabel if set
				if len(cfg.MetricsRelabelConfigs) > 0 {
					tmp, err := promclient.NewMetricsRelabelClient(apiClient, cfg.MetricsRelabelConfigs)
					if err != nil {
						return err
					}
					apiClient = tmp

				}

				// Add LabelFilter if configured
				if cfg.LabelFilterConfig != nil {
					apiClient, err = promclient.NewLabelFilterClient(ctx, apiClient, cfg.LabelFilterConfig)
					if err != nil {
						return err
					}
				}

				// Add wrap for the specific target, and add to the list
				apiClients = append(apiClients, &promclient.ErrorWrap{apiClient, "error in target=" + u.String()})
			}
		}
	}

	apiClientMetricFunc := func(i int, api, status string, took float64) {
		serverGroupSummary.WithLabelValues(targets[i], api, status).Observe(took)
	}

	logrus.Debugf("Updating targets from discovery manager: %v", targets)
	apiClient, err := promclient.NewMultiAPI(apiClients, cfg.GetAntiAffinity(), cfg.AntiAffinityDynamic, apiClientMetricFunc, 1, cfg.GetPreferMax())
	if err != nil {
		return err
	}

	newState := &ServerGroupState{
		Targets: targets,
		// Add error wrap for this specific servergroup
		apiClient: &promclient.ErrorWrap{apiClient, fmt.Sprintf("error in servergroup ord=%d", cfg.Ordinal)},
		ctx:       ctx,
		ctxCancel: ctxCancel,
	}

	if cfg.IgnoreError {
		newState.apiClient = &promclient.IgnoreErrorAPI{newState.apiClient}
	}

	if cfg.DowngradeError {
		newState.apiClient = &promclient.DowngradeErrorAPI{newState.apiClient}
	}

	s.logTargetTransition(cfg, oldCount, len(targets), !s.loaded)
	serverGroupTargets.WithLabelValues(strconv.Itoa(cfg.Ordinal), cfg.Name).Set(float64(len(targets)))

	s.state.Store(newState) // Store new state
	if oldState != nil {
		oldState.ctxCancel() // Cancel the old state
	}

	if !s.loaded {
		s.loaded = true
		close(s.Ready)
	}

	return nil
}

// newDownstreamTransport builds the base *http.Transport used to talk to the
// hosts in this server group. It is split out from ApplyConfig so it can be
// exercised directly by tests -- once ApplyConfig has layered the auth
// round-trippers (sigv4/bearer/basic-auth) on top, the underlying transport is
// no longer reachable without reflection.
func newDownstreamTransport(cfg *Config) (*http.Transport, error) {
	// Copy/paste from upstream prometheus/common until https://github.com/prometheus/common/issues/144 is resolved
	tlsConfig, err := config_util.NewTLSConfig(&cfg.HTTPConfig.HTTPConfig.TLSConfig)
	if err != nil {
		return nil, errors.Wrap(err, "error loading TLS client config")
	}
	// The dialer's address family and dual-stack (RFC 6555 "Happy Eyeballs")
	// fallback timing are configurable so downstream hosts that resolve to both
	// IPv4 and IPv6 can be reached even when one family is unreachable.
	dialer := &net.Dialer{
		Timeout:       cfg.HTTPConfig.DialTimeout,
		FallbackDelay: cfg.HTTPConfig.FallbackDelay,
	}
	// http.Transport always calls DialContext with network "tcp"; override it so
	// dial_network (tcp/tcp4/tcp6) can force a specific address family.
	dialNetwork := cfg.HTTPConfig.DialNetworkOrDefault()
	dialContext := func(ctx context.Context, _, addr string) (net.Conn, error) {
		return dialer.DialContext(ctx, dialNetwork, addr)
	}
	// The only timeout we care about is the configured scrape timeout.
	// It is applied on request. So we leave out any timings here.
	return &http.Transport{
		Proxy: http.ProxyURL(cfg.HTTPConfig.HTTPConfig.ProxyURL.URL),
		// NOTE: these two bound idle *connections*, which only means what you'd
		// expect under HTTP/1.1. With http_client.enable_http2 all requests to a
		// host are multiplexed over a single connection, so per-host concurrency
		// is then governed by the peer's SETTINGS_MAX_CONCURRENT_STREAMS instead
		// of MaxIdleConnsPerHost.
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost, // see https://github.com/golang/go/issues/13801
		DisableKeepAlives:   false,
		TLSClientConfig:     tlsConfig,
		// Setting TLSClientConfig and DialContext both make net/http
		// conservatively skip its automatic HTTP/2 wiring, so HTTP/2 is only
		// ever negotiated (via TLS ALPN, i.e. for https targets) when the
		// operator opts in with http_client.enable_http2. See the field docs on
		// HTTPClientConfig.HTTP2Enabled for the trade-offs.
		ForceAttemptHTTP2: cfg.HTTPConfig.HTTP2Enabled(),
		// 5 minutes is typically above the maximum sane scrape interval. So we can
		// use keepalive for all configurations.
		IdleConnTimeout:       cfg.IdleConnTimeout,
		DialContext:           dialContext,
		ResponseHeaderTimeout: cfg.Timeout,
	}, nil
}

// ApplyConfig applies new configuration to the ServerGroup
func (s *ServerGroup) ApplyConfig(cfg *Config) error {
	transport, err := newDownstreamTransport(cfg)
	if err != nil {
		return err
	}
	var rt http.RoundTripper = transport

	// If SigV4 is configured, wrap the transport with SigV4 round tripper
	if cfg.HTTPConfig.SigV4Config != nil {
		rt, err = sigv4.NewSigV4RoundTripper(cfg.HTTPConfig.SigV4Config, rt)
		if err != nil {
			return errors.Wrap(err, "error creating SigV4 round tripper")
		}
	}

	// If a bearer token is provided, create a round tripper that will set the
	// Authorization header correctly on each request.
	if len(cfg.HTTPConfig.HTTPConfig.BearerToken) > 0 {
		rt = config_util.NewAuthorizationCredentialsRoundTripper("Bearer", config_util.NewInlineSecret(string(cfg.HTTPConfig.HTTPConfig.BearerToken)), rt)
	} else if len(cfg.HTTPConfig.HTTPConfig.BearerTokenFile) > 0 {
		rt = config_util.NewAuthorizationCredentialsRoundTripper("Bearer", config_util.NewFileSecret(cfg.HTTPConfig.HTTPConfig.BearerTokenFile), rt)
	}

	if cfg.HTTPConfig.HTTPConfig.BasicAuth != nil {
		var passwordSecret config_util.SecretReader
		switch {
		case len(cfg.HTTPConfig.HTTPConfig.BasicAuth.Password) > 0:
			passwordSecret = config_util.NewInlineSecret(string(cfg.HTTPConfig.HTTPConfig.BasicAuth.Password))
		case len(cfg.HTTPConfig.HTTPConfig.BasicAuth.PasswordFile) > 0:
			passwordSecret = config_util.NewFileSecret(cfg.HTTPConfig.HTTPConfig.BasicAuth.PasswordFile)
		}
		rt = config_util.NewBasicAuthRoundTripper(
			config_util.NewInlineSecret(cfg.HTTPConfig.HTTPConfig.BasicAuth.Username),
			passwordSecret,
			rt,
		)
	}

	// Publish the client before the config so that any reader which sees the
	// new config also sees the client that was built from it.
	s.client.Store(&http.Client{Transport: rt})
	s.cfg.Store(cfg)

	if err := s.targetManager.ApplyConfig(map[string]discovery.Configs{"foo": cfg.ServiceDiscoveryConfigs}); err != nil {
		return err
	}

	// Start the metadata cache if histogram routing is configured to use it.
	// The cache's start is idempotent — repeated ApplyConfig calls won't
	// spawn duplicate goroutines.
	s.histogramCache.start(
		s.ctx,
		func() promclient.API {
			st := s.State()
			if st == nil {
				return nil
			}
			return st.apiClient
		},
		cfg.NativeHistogram.MetadataRefresh,
		logrus.WithFields(logrus.Fields{
			"component": "histogram-metadata-cache",
			"sg":        cfg.Ordinal,
		}),
	)
	return nil
}

// State returns the current ServerGroupState
func (s *ServerGroup) State() *ServerGroupState {
	tmp := s.state.Load()
	if ret, ok := tmp.(*ServerGroupState); ok {
		return ret
	}
	return nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *ServerGroup) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	return s.State().apiClient.GetValue(ctx, start, end, matchers)
}

// Query performs a query for the given time.
func (s *ServerGroup) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return s.State().apiClient.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *ServerGroup) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return s.State().apiClient.QueryRange(ctx, query, r)
}

// LabelValues performs a query for the values of the given label.
func (s *ServerGroup) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return s.State().apiClient.LabelValues(ctx, label, matchers, startTime, endTime)
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *ServerGroup) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	return s.State().apiClient.LabelNames(ctx, matchers, startTime, endTime)
}

// Series finds series by label matchers.
func (s *ServerGroup) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return s.State().apiClient.Series(ctx, matches, startTime, endTime)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (s *ServerGroup) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	return s.State().apiClient.Metadata(ctx, metric, limit)
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (s *ServerGroup) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	return s.State().apiClient.QueryExemplars(ctx, query, startTime, endTime)
}
