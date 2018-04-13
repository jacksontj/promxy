package test

import (
	"io/ioutil"
	"net/http"
	"path/filepath"
	"testing"

	yaml "gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/proxystorage"
	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/util/testutil"
	"github.com/prometheus/prometheus/web/api/v1"
	"github.com/sirupsen/logrus"
)

const rawPSConfig = `
promxy:
  http_client:
    tls_config:
      insecure_skip_verify: true
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083`

const rawDoublePSConfig = `
promxy:
  http_client:
    tls_config:
      insecure_skip_verify: true
  server_groups:
    - static_configs:
        - targets:
          - localhost:8083
      labels:
        az: a
    - static_configs:
        - targets:
          - localhost:8084
      labels:
        az: b`

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

func startAPIForTest(test *promql.Test, listen string) (*http.Server, chan struct{}) {
	// Start up API server for engine
	cfgFunc := func() config.Config { return config.DefaultConfig }
	// Return 503 until ready (for us there isn't much startup, so this might not need to be implemented
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(test.QueryEngine(), test.Storage(), nil, nil, cfgFunc, readyFunc)

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
	for _, fn := range files {
		test, err := newTestFromFile(t, fn)
		if err != nil {
			t.Errorf("error creating test for %s: %s", fn, err)
		}

		// Create API for the storage engine
		srv, stopChan := startAPIForTest(test, ":8083")
		// Create ProxyStorage against it
		ps := getProxyStorage(rawPSConfig)

		// Replace the test engine with the promxy one
		eOpts := promql.EngineOptions{}
		eOpts = *promql.DefaultEngineOptions
		engine := promql.NewEngine(ps, &eOpts)
		test.SetQueryEngine(engine)

		err = test.Run()
		if err != nil {
			t.Errorf("error running test %s: %s", fn, err)
		}
		test.Close()

		// stop server
		srv.Shutdown(nil)
		<-stopChan
	}
}

func TestEvaluations(t *testing.T) {
	files, err := filepath.Glob("testdata/*.test")
	if err != nil {
		t.Fatal(err)
	}
	for _, fn := range files {
		test, err := newTestFromFile(t, fn)
		if err != nil {
			t.Errorf("error creating test for %s: %s", fn, err)
		}

		// Create API for the storage engine
		srv, stopChan := startAPIForTest(test, ":8083")
		srv2, stopChan2 := startAPIForTest(test, ":8084")

		// Create ProxyStorage against it
		ps := getProxyStorage(rawDoublePSConfig)

		// Replace the test engine with the promxy one
		eOpts := promql.EngineOptions{}
		eOpts = *promql.DefaultEngineOptions
		engine := promql.NewEngine(ps, &eOpts)
		test.SetQueryEngine(engine)

		err = test.Run()
		if err != nil {
			t.Errorf("error running test %s: %s", fn, err)
		}
		test.Close()

		// stop server
		srv.Shutdown(nil)
		srv2.Shutdown(nil)

		<-stopChan
		<-stopChan2
	}
}

func newTestFromFile(t testutil.T, filename string) (*promql.Test, error) {
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return promql.NewTest(t, string(content))
}
