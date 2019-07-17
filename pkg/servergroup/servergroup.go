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
	"github.com/prometheus/prometheus/pkg/relabel"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/promclient"

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

// New creates a new servergroup
func New() *ServerGroup {
	ctx, ctxCancel := context.WithCancel(context.Background())
	// Create the targetSet (which will maintain all of the updating etc. in the background)
	sg := &ServerGroup{
		ctx:       ctx,
		ctxCancel: ctxCancel,
		Ready:     make(chan struct{}),
	}

	logCfg := &promlog.Config{
		Level:  &promlog.AllowedLevel{},
		Format: &promlog.AllowedFormat{},
	}
	if err := logCfg.Level.Set("info"); err != nil {
		panic(err)
	}
	sg.targetManager = discovery.NewManager(ctx, promlog.New(logCfg))
	// Background the updating
	go sg.targetManager.Run()
	go sg.Sync()

	return sg

}

// ServerGroupState encapsulates the state of a serverGroup from service discovery
type ServerGroupState struct {
	// Targets is the list of target URLs for this discovery round
	Targets   []string
	apiClient promclient.API
}

// ServerGroup encapsulates a set of prometheus downstreams to query/aggregate
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

// Cancel stops backround processes (e.g. discovery manager)
func (s *ServerGroup) Cancel() {
	s.ctxCancel()
}

// Sync updates the targets from our discovery manager
func (s *ServerGroup) Sync() {
	syncCh := s.targetManager.SyncCh()

SYNC_LOOP:
	for targetGroupMap := range syncCh {
		logrus.Debug("Updating targets from discovery manager")
		targets := make([]string, 0)
		apiClients := make([]promclient.API, 0)

		for _, targetGroupList := range targetGroupMap {
			for _, targetGroup := range targetGroupList {
				for _, target := range targetGroup.Targets {
					lbls := make([]labels.Label, 0, len(target)+len(targetGroup.Labels))

					for ln, lv := range target {
						lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
					}

					for ln, lv := range targetGroup.Labels {
						if _, ok := target[ln]; !ok {
							lbls = append(lbls, labels.Label{Name: string(ln), Value: string(lv)})
						}
					}

					lset := labels.New(lbls...)
					logrus.Tracef("Potential target pre-relabel: %v", lset)
					lset = relabel.Process(lset, s.Cfg.RelabelConfigs...)
					logrus.Tracef("Potential target post-relabel: %v", lset)
					// Check if the target was dropped, if so we skip it
					if len(lset) == 0 {
						continue
					}

					// If there is no address, then we can't use this set of targets
					if v := lset.Get(model.AddressLabel); v == "" {
						logrus.Errorf("Discovery target is missing address label: %v", lset)
						continue SYNC_LOOP
					}

					u := &url.URL{
						Scheme: string(s.Cfg.GetScheme()),
						Host:   string(lset.Get(model.AddressLabel)),
						Path:   s.Cfg.PathPrefix,
					}
					targets = append(targets, u.Host)

					client, err := api.NewClient(api.Config{Address: u.String(), RoundTripper: s.Client.Transport})
					if err != nil {
						panic(err) // TODO: shouldn't be possible? If this happens I guess we log and skip?
					}

					var apiClient promclient.API
					apiClient = &promclient.PromAPIV1{v1.NewAPI(client)}

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

						apiClient = &promclient.PromAPIRemoteRead{apiClient, remoteStorageClient}
					}

					// Optionally add time range layers
					if s.Cfg.AbsoluteTimeRangeConfig != nil {
						apiClient = &promclient.AbsoluteTimeFilter{
							API:   apiClient,
							Start: s.Cfg.AbsoluteTimeRangeConfig.Start,
							End:   s.Cfg.AbsoluteTimeRangeConfig.End,
						}
					}

					if s.Cfg.RelativeTimeRangeConfig != nil {
						apiClient = &promclient.RelativeTimeFilter{
							API:   apiClient,
							Start: s.Cfg.RelativeTimeRangeConfig.Start,
							End:   s.Cfg.RelativeTimeRangeConfig.End,
						}
					}

					// We remove all private labels after we set the target entry
					modelLabelSet := make(model.LabelSet, len(lset))
					for _, lbl := range lset {
						if !strings.HasPrefix(string(lbl.Name), model.ReservedLabelPrefix) {
							modelLabelSet[model.LabelName(lbl.Name)] = model.LabelValue(lbl.Value)
						}
					}

					// Add labels
					apiClient = &promclient.AddLabelClient{apiClient, modelLabelSet.Merge(s.Cfg.Labels)}

					// If debug logging is enabled, wrap the client with a debugAPI client
					// Since these are called in the reverse order of what we add, we want
					// to make sure that this is the last wrap of the client
					if logrus.GetLevel() >= logrus.DebugLevel {
						apiClient = &promclient.DebugAPI{apiClient, u.String()}
					}

					apiClients = append(apiClients, apiClient)
				}
			}
		}

		apiClientMetricFunc := func(i int, api, status string, took float64) {
			serverGroupSummary.WithLabelValues(targets[i], api, status).Observe(took)
		}

		logrus.Debugf("Updating targets from discovery manager: %v", targets)
		newState := &ServerGroupState{
			Targets:   targets,
			apiClient: promclient.NewMultiAPI(apiClients, s.Cfg.GetAntiAffinity(), apiClientMetricFunc, 1),
		}

		if s.Cfg.IgnoreError {
			newState.apiClient = &promclient.IgnoreErrorAPI{newState.apiClient}
		}

		s.state.Store(newState)

		if !s.loaded {
			s.loaded = true
			close(s.Ready)
		}
	}
}

// ApplyConfig applies new configuration to the ServerGroup
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

// State returns the current ServerGroupState
func (s *ServerGroup) State() *ServerGroupState {
	tmp := s.state.Load()
	if ret, ok := tmp.(*ServerGroupState); ok {
		return ret
	}
	return nil
}

// GetValue loads the raw data for a given set of matchers in the time range
func (s *ServerGroup) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	return s.State().apiClient.GetValue(ctx, start, end, matchers)
}

// Query performs a query for the given time.
func (s *ServerGroup) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	return s.State().apiClient.Query(ctx, query, ts)
}

// QueryRange performs a query for the given range.
func (s *ServerGroup) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	return s.State().apiClient.QueryRange(ctx, query, r)
}

// LabelValues performs a query for the values of the given label.
func (s *ServerGroup) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	return s.State().apiClient.LabelValues(ctx, label)
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (s *ServerGroup) LabelNames(ctx context.Context) ([]string, api.Warnings, error) {
	return s.State().apiClient.LabelNames(ctx)
}

// Series finds series by label matchers.
func (s *ServerGroup) Series(ctx context.Context, matches []string, startTime, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	return s.State().apiClient.Series(ctx, matches, startTime, endTime)
}
