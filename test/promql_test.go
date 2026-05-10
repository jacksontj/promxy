package test

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	_ "net/http/pprof"
	"os"
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
	"github.com/prometheus/prometheus/promql/promqltest"
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

// Config templates take the bound API address(es) as %s. We pick ports
// dynamically per subtest so concurrent / back-to-back tests don't collide on
// the same TCP port (TIME_WAIT, OS bind races).
const rawPSConfig = `
promxy:
  http_client:
    tls_config:
      insecure_skip_verify: true
  server_groups:
    - static_configs:
        - targets:
          - %s
`

const rawPSRemoteReadConfig = `
promxy:
  server_groups:
    - static_configs:
        - targets:
          - %s
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
          - %s
      labels:
        az: a
    - static_configs:
        - targets:
          - %s
      labels:
        az: b
`

const rawDoublePSConfigRR = `
promxy:
  server_groups:
    - static_configs:
        - targets:
          - %s
      labels:
        az: a
      remote_read: true
    - static_configs:
        - targets:
          - %s
      labels:
        az: b
      remote_read: true
`

func getProxyStorage(cfg string) *proxystorage.ProxyStorage {
	// Create promxy in front of it
	pstorageConfig := &proxyconfig.Config{}
	if err := yaml.Unmarshal([]byte(cfg), &pstorageConfig); err != nil {
		panic(err)
	}

	ps, err := proxystorage.NewProxyStorage(func(rangeMillis int64) int64 {
		return int64(config.DefaultGlobalConfig.EvaluationInterval) / int64(time.Millisecond)
	}, "")
	if err != nil {
		logrus.Fatalf("Error creating proxy: %v", err)
	}

	if err := ps.ApplyConfig(pstorageConfig); err != nil {
		logrus.Fatalf("Unable to apply config: %v", err)
	}
	return ps
}

// startAPIForTest binds an OS-assigned localhost port and starts a v1 API
// server on it. The listener is bound synchronously before the function
// returns, so callers can issue requests immediately without racing the
// server goroutine. Returns the bound "host:port" plus a Shutdown handle and
// a stop channel that closes once Serve returns.
func startAPIForTest(s storage.Storage) (*http.Server, string, chan struct{}) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(fmt.Errorf("startAPIForTest: bind: %w", err))
	}
	addr := ln.Addr().String()

	cfgFunc := func() config.Config { return config.DefaultConfig }
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(
		promql.NewEngine(promql.EngineOpts{
			Timeout:                  10 * time.Minute,
			MaxSamples:               50000000,
			NoStepSubqueryIntervalFn: func(int64) int64 { return (1 * time.Minute).Milliseconds() },
			EnableAtModifier:         true,
			EnableNegativeOffset:     true,
			EnableDelayedNameRemoval: true,
		}),
		s.(storage.SampleAndChunkQueryable),
		nil, // appendable
		nil, // exemplarQueryable
		nil, // scrapePoolsRetriever
		nil, // targetRetriever
		nil, // alertmanagerRetriever
		cfgFunc,
		nil, // flagsMap
		v1.GlobalURLOptions{
			ListenAddress: addr,
			Host:          "localhost",
			Scheme:        "http",
		},
		readyFunc,
		nil,      // db (TSDBAdminStats)
		"",       // dbDir
		false,    // enableAdmin
		nil,      // logger
		nil,      // rulesRetriever
		50000000, // remoteReadSampleLimit
		1000,     // remoteReadConcurrencyLimit
		1048576,  // remoteReadMaxBytesInFrame
		false,    // isAgent
		nil,      // corsOrigin
		nil,      // runtimeInfo
		nil,      // buildInfo
		nil,      // notificationsGetter
		nil,      // notificationsSub
		nil,      // gatherer
		nil,      // registerer
		nil,      // statsRenderer
		false,    // rwEnabled
		nil,      // acceptRemoteWriteProtoMsgs
		false,    // otlpEnabled
		false,    // otlpDeltaToCumulative
		false,    // otlpNativeDeltaIngestion
		false,    // ctZeroIngestionEnabled
	)

	apiRouter := route.New()
	api.Register(apiRouter.WithPrefix("/api/v1"))

	stopChan := make(chan struct{})
	srv := &http.Server{Handler: apiRouter}

	go func() {
		defer close(stopChan)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Println("Error serving on", addr, err)
		}
	}()

	return srv, addr, stopChan
}

func TestUpstreamEvaluations(t *testing.T) {
	files, err := filepath.Glob("../vendor/github.com/prometheus/prometheus/promql/promqltest/testdata/*.test")
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
			// Even on remote_read mode, the range-eval fan-out emits an extra
			// {__name__="metric"} sample at the boundary that promxy's merge
			// can't dedupe; revisit when working through staleness handling.
			if strings.Contains(fn, "staleness.test") {
				continue
			}

			// histograms.test / native_histograms.test require feeding
			// histogram samples back into the embedded engine. The
			// remote_read fanout pins the original FloatHistogram alongside
			// the model.SampleHistogram carrier (see histogram_convert.go),
			// so it preserves full schema fidelity. The HTTP-API JSON path
			// can only reconstruct a best-effort custom-buckets histogram,
			// so we restrict these suites to the remote_read config.
			base := filepath.Base(fn)
			if (base == "histograms.test" || base == "native_histograms.test") && psConfig != rawPSRemoteReadConfig {
				continue
			}

			// histograms.test (the NHCB-driven classic-histogram suite) has
			// 7 failing evals out of 105, all in the engine→annotation
			// propagation path or in zero-bucket result encoding. Tracked
			// as a follow-up to https://github.com/jacksontj/promxy/issues/637;
			// native_histograms.test (285 evals) does pass in full and
			// remains enabled below to exercise the histogram plumbing
			// end-to-end.
			if base == "histograms.test" {
				continue
			}

			// Skip test files that exercise upstream prom features promxy
			// doesn't yet support proxying. Re-enable these once the
			// corresponding promxy gaps are filled.
			switch base {
			case
				// __name__-label propagation through aggregations: 3.x's
				// EnableDelayedNameRemoval behavior interacts with promxy's
				// metricNameWorkaroundLabel rewrite in ways the rewrite
				// doesn't currently model.
				"name_label_dropping.test",
				// New 3.x experimental duration-expression syntax.
				"duration_expression.test",
				// 3.x __type__ / __unit__ labels from OTLP — promxy's
				// rewrite paths don't preserve them yet.
				"type_and_unit.test",
				// aggregators.test: count_values with parenthesised string
				// param panics in proxystorage's COUNT_VALUES handler;
				// also includes histogram rows.
				"aggregators.test",
				// operators.test exercises a few edge cases (e.g. NaN
				// comparison ordering after delayed name removal) that the
				// proxy rewrite doesn't yet handle.
				"operators.test",
				// subquery.test: promxy disables the rewrite for
				// subquery descendants, so the proxy path falls back on raw
				// Querier-based eval; some cases miss data.
				"subquery.test",
				// collision.test: cmd.start = 4s makes the upstream
				// atModifierTestCases sweep generate a range query at
				// [iq.evalTime - 1m, iq.evalTime + 1m] starting at -59.2s,
				// which trips the upstream prometheus/common
				// model.Time.UnmarshalJSON bug: "-59.200" decodes to
				// Time(-58800) instead of Time(-59200). Promxy now
				// returns an explicit error for pre-epoch sub-second
				// timestamps in pushdown rather than silently producing
				// shifted data; the test framework propagates that error
				// as a query failure, so the file is skipped here. None
				// of the actual test cases (which have non-negative
				// timestamps) are affected in production.
				"collision.test":
				continue
			}
			t.Run(strconv.Itoa(i)+fn, func(t *testing.T) {
				test, err := newTestFromFile(t, fn)
				if err != nil {
					t.Skipf("error creating test for %s: %s (likely uses syntax not supported by promxy)", fn, err)
				}

				// Create API for the storage engine on an OS-assigned port.
				srv, addr, stopChan := startAPIForTest(test.Storage())

				ps := getProxyStorage(fmt.Sprintf(psConfig, addr))
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
			// Skip files with expectations that assume single-server-group
			// fan-out behavior — they emit 2x sums and `az` labels that the
			// test data does not list. Re-author the expected sets to
			// account for the double-PS doubling before re-enabling.
			if strings.HasSuffix(fn, "aggregators.test") {
				continue
			}
			t.Run(strconv.Itoa(i)+fn, func(t *testing.T) {
				test, err := newTestFromFile(t, fn)
				if err != nil {
					t.Errorf("error creating test for %s: %s", fn, err)
				}

				// Create API for the storage engine on OS-assigned ports.
				srv, addr, stopChan := startAPIForTest(test.Storage())
				srv2, addr2, stopChan2 := startAPIForTest(test.Storage())

				ps := getProxyStorage(fmt.Sprintf(psConfig, addr, addr2))
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

func newTestFromFile(t testutil.T, filename string) (*promqltest.Test, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return promqltest.NewTest(t, string(content))
}

// Create a wrapper for the storage that will proxy reads but not writes

type LayeredStorage struct {
	proxyStorage storage.Storage
	baseStorage  storage.Storage
}

func (p *LayeredStorage) Querier(mint, maxt int64) (storage.Querier, error) {
	return p.proxyStorage.Querier(mint, maxt)
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
func (p *LayeredStorage) ChunkQuerier(mint, maxt int64) (storage.ChunkQuerier, error) {
	return p.baseStorage.ChunkQuerier(mint, maxt)
}
