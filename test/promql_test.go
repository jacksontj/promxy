package test

import (
	"context"
	"io/ioutil"
	"log"
	"net/http"
	_ "net/http/pprof"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	_ "github.com/prometheus/prometheus/discovery/install" // Register service discovery implementations.
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/testutil"
	v1 "github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/proxystorage"
)

func init() {
	go func() {
		log.Println(http.ListenAndServe("localhost:6060", nil))
	}()
}

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
	// Create promxy in front of it
	pstorageConfig := &proxyconfig.Config{}
	if err := yaml.Unmarshal([]byte(cfg), &pstorageConfig); err != nil {
		panic(err)
	}

	ps, err := proxystorage.NewProxyStorage(func(rangeMillis int64) int64 {
		return int64(config.DefaultGlobalConfig.EvaluationInterval) / int64(time.Millisecond)
	})
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
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
			Timeout:    10 * time.Minute,
			MaxSamples: 50000000,
		}),
		s.(storage.SampleAndChunkQueryable),
		nil, //factoryTr
		nil, //factoryAr
		cfgFunc,
		nil, // flags
		v1.GlobalURLOptions{
			ListenAddress: listen,
			Host:          "localhost",
			Scheme:        "http",
		}, // global URL options
		readyFunc, // ready
		nil,       // local storage
		"",        //tsdb dir
		false,     // enable admin API
		nil,       // logger
		nil,       // FactoryRr
		50000000,  // RemoteReadSampleLimit
		1000,      // RemoteReadConcurrencyLimit
		1048576,   // RemoteReadBytesInFrame
		nil,       // CORSOrigin
		nil,       // runtimeInfo
		nil,       // versionInfo
		nil,       // gatherer
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

func (p *LayeredStorage) Appender(ctx context.Context) storage.Appender {
	return p.baseStorage.Appender(ctx)
}
func (p *LayeredStorage) Close() error {
	return p.baseStorage.Close()
}
func (p *LayeredStorage) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return p.baseStorage.ChunkQuerier(ctx, mint, maxt)
}
