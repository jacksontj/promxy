package servergroup

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
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
	"github.com/prometheus/prometheus/relabel"
	"github.com/prometheus/prometheus/storage/remote"
	httputil "github.com/prometheus/prometheus/util/httputil"

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
	Client        *http.Client
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

// TODO: move config + client into state object to be swapped with atomics
func (s *ServerGroup) ApplyConfig(cfg *Config) error {
	s.Cfg = cfg

	// stage the client swap as well
	// TODO: better name
	client, err := httputil.NewClientFromConfig(cfg.HTTPConfig, "somename")
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
	transport := client.Transport.(*http.Transport)
	transport.DialContext = (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext

	s.Client = client

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

func (s *ServerGroup) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, error) {
	if s.Cfg.RemoteRead {
		return s.RemoteRead(ctx, start, end, matchers)
	} else {
		// http://localhost:8080/api/v1/query?query=scrape_duration_seconds%7Bjob%3D%22prometheus%22%7D&time=1507412244.663&_=1507412096887
		pql, err := promhttputil.MatcherToString(matchers)
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
	targets := s.Targets()

	childContext, childContextCancel := context.WithCancel(ctx)
	defer childContextCancel()
	resultChan := make(chan model.Value, len(targets))
	errChan := make(chan error, len(targets))

	for _, target := range targets {
		parsedUrl, err := url.Parse(target + "/api/v1/read")
		if err != nil {
			return nil, err
		}
		go func(stringUrl *url.URL) {
			cfg := &remote.ClientConfig{
				URL: &config_util.URL{parsedUrl},
				// TODO: from context?
				Timeout: model.Duration(time.Minute * 2),
			}
			client, err := remote.NewClient(1, cfg)
			if err != nil {
				errChan <- err
				return
			}

			start := time.Now()
			query, err := remote.ToQuery(int64(timestamp.FromTime(start)), int64(timestamp.FromTime(end)), matchers)
			if err != nil {
				errChan <- err
				return
			}
			// http://localhost:8083/api/v1/query?query=%7B__name__%3D%22metric%22%7D%5B302s%5D&time=21
			result, err := client.Read(childContext, query)
			took := time.Now().Sub(start)

			if err != nil {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "remoteread", "error").Observe(float64(took))
				errChan <- err
			} else {
				serverGroupSummary.WithLabelValues(parsedUrl.Host, "remoteread", "success").Observe(float64(took))
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

				err = promhttputil.ValueAddLabelSet(matrix, s.Cfg.Labels)
				if err != nil {
					errChan <- err
					return
				}

				resultChan <- matrix
			}

		}(parsedUrl)
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
			// TODO: check qData.ResultType
			if result == nil {
				result = childResult
			} else {
				var err error
				result, err = promhttputil.MergeValues(s.Cfg.GetAntiAffinity(), result, childResult)
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

//
func (s *ServerGroup) GetData(ctx context.Context, path string, values url.Values) (model.Value, error) {
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
			result, err := promclient.GetData(childContext, stringUrl, s.Client, s.Cfg.Labels)
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

func (s *ServerGroup) GetValuesForLabelName(ctx context.Context, path string) (*promclient.LabelResult, error) {
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
			result, err := promclient.GetValuesForLabelName(childContext, stringUrl, s.Client)
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
