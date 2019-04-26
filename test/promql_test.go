package test

import (
	"context"
	"io/ioutil"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/testutil"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"

	proxyconfig "github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/proxystorage"
)

const rawPSConfig = `
promxy:
  http_client:
    tls_config:
      insecure_skip_verify: true
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083
`

const rawPSRemoteReadConfig = `
promxy:
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083
      remote_read: true
      http_client:
        tls_config:
          insecure_skip_verify: true
`

const rawDoublePSConfig = `
promxy:
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083
      labels:
        az: a
      http_client:
        tls_config:
          insecure_skip_verify: true
    - static_configs:
        - targets:
          - localhost:8084
      labels:
        az: b
      http_client:
        tls_config:
          insecure_skip_verify: true
`

const rawDoublePSConfigRR = `
promxy:
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083
      labels:
        az: a
      remote_read: true
      http_client:
        tls_config:
          insecure_skip_verify: true
    - static_configs:
        - targets:
          - localhost:8084
      labels:
        az: b
      remote_read: true
      http_client:
        tls_config:
          insecure_skip_verify: true
`

func getProxyStorage(cfg string) *proxystorage.ProxyStorage {
	ps, err := proxystorage.NewProxyStorage()
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}

	// Create promxy in front of it
	pstorageConfig := &proxyconfig.Config{}
	if err := yaml.Unmarshal([]byte(cfg), &pstorageConfig); err != nil {
		panic(err)
	}
	if err := ps.ApplyConfig(pstorageConfig); err != nil {
		logrus.Fatalf("Unable to apply config: %v", err)
	}
	return ps
}

func startAPIForTest(s storage.Storage, listen string) (*http.Server, chan struct{}) {
	// Start up API server for engine
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(
		promql.NewEngine(promql.EngineOpts{
		    MaxConcurrent: 20,
		    Timeout: 10*time.Minute,
		    MaxSamples: 50000000,
		}),
		s.(storage.Queryable),
		nil,
		nil,
		cfgFunc,
		nil,
		readyFunc,
		nil,
		true,
		nil,
		nil,
		50000000,
		1000,
		nil,
	)

	apiRouter := route.New()
	api.Register(apiRouter.WithPrefix("/api/v1"))

	startChan := make(chan struct{})
	stopChan := make(chan struct{})
	srv := &http.Server{Addr: listen, Handler: apiRouter}

	go func() {
		defer close(stopChan)
		close(startChan)
		srv.ListenAndServe()
	}()

	<-startChan

	return srv, stopChan
}

func TestUpstreamEvaluations(t *testing.T) {
	files, err := filepath.Glob("../vendor/github.com/prometheus/prometheus/promql/testdata/*.test")
	if err != nil {
		t.Fatal(err)
	}
	for i, psConfig := range []string{rawPSConfig, rawPSRemoteReadConfig} {
		for _, fn := range files {

			// Upstream prom is using a StaleNan to determine if a given timeseries has gone
			// NaN -- the problem being that for range vectors they filter out all "stale" samples
			// meaning that it isn't possible to get a "raw" dump of data through the v1 API
			// The only option that exists in reality is the "remote read" API -- which suffers
			// from the same memory-balooning problems that the HTTP+JSON API originally had.
			// It has **less** of a problem (its 2x memory instead of 14x) so it is a viable option.
			// NOTE: Skipped only when promxy isn't configured to use the remote_read API
			if psConfig == rawPSConfig && strings.Contains(fn, "staleness.test") {
				continue
			}
			t.Run(strconv.Itoa(i)+fn, func(t *testing.T) {
				test, err := newTestFromFile(t, fn)
				if err != nil {
					t.Errorf("error creating test for %s: %s", fn, err)
				}

				// Create API for the storage engine
				srv, stopChan := startAPIForTest(test.Storage(), ":8083")

				ps := getProxyStorage(psConfig)
				lStorage := &LayeredStorage{ps, test.Storage()}
				// Replace the test storage with the promxy one
				test.SetStorage(lStorage)
				test.QueryEngine().NodeReplacer = ps.NodeReplacer

				err = test.Run()
				if err != nil {
					t.Errorf("error running test %s: %s", fn, err)
				}
				test.Close()

				// stop server
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				srv.Shutdown(ctx)
				<-stopChan
			})
		}
	}
}

func TestEvaluations(t *testing.T) {
	files, err := filepath.Glob("testdata/*.test")
	if err != nil {
		t.Fatal(err)
	}
	for i, psConfig := range []string{rawDoublePSConfig, rawDoublePSConfigRR} {
		for _, fn := range files {
			t.Run(strconv.Itoa(i)+fn, func(t *testing.T) {
				test, err := newTestFromFile(t, fn)
				if err != nil {
					t.Errorf("error creating test for %s: %s", fn, err)
				}

				// Create API for the storage engine
				srv, stopChan := startAPIForTest(test.Storage(), ":8083")
				srv2, stopChan2 := startAPIForTest(test.Storage(), ":8084")

				ps := getProxyStorage(psConfig)
				lStorage := &LayeredStorage{ps, test.Storage()}
				// Replace the test storage with the promxy one
				test.SetStorage(lStorage)
				test.QueryEngine().NodeReplacer = ps.NodeReplacer

				err = test.Run()
				if err != nil {
					t.Errorf("error running test %s: %s", fn, err)
				}

				test.Close()

				// stop server
				ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
				defer cancel()
				srv.Shutdown(ctx)
				srv2.Shutdown(ctx)

				<-stopChan
				<-stopChan2
			})
		}
	}
}

func newTestFromFile(t testutil.T, filename string) (*promql.Test, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return promql.NewTest(t, string(content))
}

// Create a wrapper for the storage that will proxy reads but not writes

type LayeredStorage struct {
	proxyStorage storage.Storage
	baseStorage  storage.Storage
}

func (p *LayeredStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return p.proxyStorage.Querier(ctx, mint, maxt)
}
func (p *LayeredStorage) StartTime() (int64, error) {
	return p.baseStorage.StartTime()
}

func (p *LayeredStorage) Appender() (storage.Appender, error) {
	return p.baseStorage.Appender()
}
func (p *LayeredStorage) Close() error {
	return p.baseStorage.Close()
}
