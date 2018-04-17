package servergroup

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sync/atomic"
	"time"

	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/common/promlog"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/relabel"

	sd_config "github.com/prometheus/prometheus/discovery/config"
)

var (
	// TODO: have a marker for "which" servergroup
	serverGroupSummary = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Name: "server_group_request",
		Help: "Summary of calls to servergroup instances",
	}, []string{"host", "call", "status"})
)

func init() {
	prometheus.MustRegister(serverGroupSummary)
}

// TODO: pass in parent context
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

type ServerGroup struct {
	ctx       context.Context
	ctxCancel context.CancelFunc

	loaded bool
	Ready  chan struct{}

	Cfg           *Config
	targetManager *discovery.Manager

	OriginalURLs []string

	urls atomic.Value
}

func (s *ServerGroup) Cancel() {
	s.ctxCancel()
}

func (s *ServerGroup) Sync() {
	syncCh := s.targetManager.SyncCh()

	for targetGroupMap := range syncCh {
		targets := make([]string, 0)
		for _, targetGroupList := range targetGroupMap {
			for _, targetGroup := range targetGroupList {
				for _, target := range targetGroup.Targets {

					target = relabel.Process(target, s.Cfg.RelabelConfigs...)
					// Check if the target was dropped.
					if target == nil {
						continue
					}

					u := &url.URL{
						Scheme: string(s.Cfg.GetScheme()),
						Host:   string(target[model.AddressLabel]),
					}
					targets = append(targets, u.String())
				}
			}
		}
		s.urls.Store(targets)

		if !s.loaded {
			s.loaded = true
			close(s.Ready)
		}
	}
}

func (s *ServerGroup) ApplyConfig(cfg *Config) error {
	s.Cfg = cfg

	if err := s.targetManager.ApplyConfig(map[string]sd_config.ServiceDiscoveryConfig{"foo": cfg.Hosts}); err != nil {
		return err
	}
	return nil
}

func (s *ServerGroup) Targets() []string {
	tmp := s.urls.Load()
	if ret, ok := tmp.([]string); ok {
		return ret
	} else {
		return nil
	}
}

//
func (s *ServerGroup) GetData(ctx context.Context, path string, values url.Values, client *http.Client) (model.Value, error) {
	targets := s.Targets()

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan *promclient.DataResult, len(targets))
	errChan := make(chan error, len(targets))

	for _, target := range targets {
		parsedUrl, err := url.Parse(target + path)
		if err != nil {
			return nil, err
		}
		parsedUrl.RawQuery = values.Encode()
		go func(stringUrl string) {
			start := time.Now()
			result, err := promclient.GetData(childContext, stringUrl, client, s.Cfg.Labels)
			took := time.Now().Sub(start)
			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "error").Observe(float64(took))
				errChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "getdata", "success").Observe(float64(took))
				resultChan <- result
			}
		}(parsedUrl.String())
	}

	// Wait for results as we get them
	var result model.Value
	var lastError error
	errCount := 0
	for i := 0; i < len(targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case err := <-errChan:
			lastError = err
			errCount++

		case childResult := <-resultChan:
			// If the server responded with a non-success, lets mark that as an error
			if childResult.Status != promhttputil.StatusSuccess {
				lastError = fmt.Errorf(childResult.Error)
				errCount++
				continue
			}

			// TODO: check qData.ResultType
			if result == nil {
				result = childResult.Data.Result
			} else {
				var err error
				result, err = promhttputil.MergeValues(s.Cfg.GetAntiAffinity(), result, childResult.Data.Result)
				if err != nil {
					return nil, err
				}
			}
		}
	}

	if errCount != 0 && errCount == len(targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}

func (s *ServerGroup) GetValuesForLabelName(ctx context.Context, path string, client *http.Client) (*promclient.LabelResult, error) {
	targets := s.Targets()

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan *promclient.LabelResult, len(targets))
	errChan := make(chan error, len(targets))

	for _, target := range targets {
		parsedUrl, err := url.Parse(target + path)
		if err != nil {
			return nil, err
		}
		go func(stringUrl string) {
			start := time.Now()
			result, err := promclient.GetValuesForLabelName(childContext, stringUrl, client)
			took := time.Now().Sub(start)
			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "label_values", "error").Observe(float64(took))
				errChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "label_values", "success").Observe(float64(took))
				resultChan <- result
			}
		}(parsedUrl.String())
	}

	// Wait for results as we get them
	result := &promclient.LabelResult{}
	var lastError error
	errCount := 0
	for i := 0; i < len(targets); i++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case err := <-errChan:
			lastError = err
			errCount++
		case childResult := <-resultChan:
			// If the server responded with a non-success, lets mark that as an error
			if childResult.Status != promhttputil.StatusSuccess {
				lastError = fmt.Errorf(childResult.Error)
				errCount++
				continue
			}

			if result == nil {
				result = childResult
			} else {
				if err := result.Merge(childResult); err != nil {
					return nil, err
				}
			}
		}
	}

	// If we got only errors, lets return that
	if errCount == len(targets) {
		return nil, fmt.Errorf("Unable to fetch from downstream servers, lastError: %s", lastError.Error())
	}

	return result, nil
}
