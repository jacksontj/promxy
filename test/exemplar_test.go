package test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"sort"
	"testing"
	"time"

	yaml "gopkg.in/yaml.v2"

	"github.com/prometheus/common/route"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/exemplar"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/util/teststorage"
	v1 "github.com/prometheus/prometheus/web/api/v1"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/proxystorage"
)

// startAPIWithExemplars boots a real prometheus v1 API server backed by a
// real test storage that supports exemplars. Returns the bound addr so the
// caller can wire it into a promxy server-group config; the test storage
// is also returned so the caller can ingest series + exemplars before
// hitting the API.
func startAPIWithExemplars(t *testing.T) (*teststorage.TestStorage, string, func()) {
	t.Helper()

	stor := teststorage.New(t)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()

	cfgFunc := func() config.Config { return config.DefaultConfig }
	readyFunc := func(f http.HandlerFunc) http.HandlerFunc { return f }

	api := v1.NewAPI(
		promql.NewEngine(promql.EngineOpts{
			Timeout:                  10 * time.Minute,
			MaxSamples:               50000000,
			NoStepSubqueryIntervalFn: func(int64) int64 { return (1 * time.Minute).Milliseconds() },
		}),
		stor,
		nil,
		stor.ExemplarQueryable(),
		nil, nil, nil,
		cfgFunc,
		nil,
		v1.GlobalURLOptions{ListenAddress: addr, Host: "localhost", Scheme: "http"},
		readyFunc,
		nil, "", false,
		nil, nil,
		50000000, 1000, 1048576,
		false,
		nil, nil, nil, nil, nil, nil, nil, nil,
		false, nil,
		false, false, false, false,
	)

	apiRouter := route.New()
	api.Register(apiRouter.WithPrefix("/api/v1"))

	srv := &http.Server{Handler: apiRouter}
	stopCh := make(chan struct{})
	go func() {
		defer close(stopCh)
		_ = srv.Serve(ln)
	}()

	stop := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		<-stopCh
		_ = stor.Close()
	}
	return stor, addr, stop
}

func ingestExemplar(t *testing.T, stor *teststorage.TestStorage, lset labels.Labels, value float64, ts int64, exLabels labels.Labels, exValue float64, exTs int64) {
	t.Helper()
	app := stor.Appender(context.Background())
	if _, err := app.Append(0, lset, ts, value); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	// teststorage builds its TSDB with EnableExemplarStorage=false by
	// default, so the head appender silently drops exemplars. Append
	// directly to the side-channel exemplar storage instead — the
	// ExemplarQueryable returned by teststorage reads from it.
	if _, err := stor.AppendExemplar(0, lset, exemplar.Exemplar{Labels: exLabels, Value: exValue, Ts: exTs, HasTs: true}); err != nil {
		t.Fatalf("AppendExemplar: %v", err)
	}
}

// TestExemplarsEndToEnd ingests series + exemplars into a real test storage
// fronted by a real Prometheus v1 API server, configures promxy to point at
// it, and verifies promxy.ProxyStorage.ExemplarQuerier (which the
// /api/v1/query_exemplars HTTP handler uses) round-trips the data.
func TestExemplarsEndToEnd(t *testing.T) {
	stor, addr, stop := startAPIWithExemplars(t)
	defer stop()

	now := time.Now().Unix() * 1000
	ingestExemplar(t,
		stor,
		labels.FromStrings(labels.MetricName, "http_requests_total", "job", "api", "code", "200"),
		1, now,
		labels.FromStrings("trace_id", "abc"), 1.5, now,
	)
	ingestExemplar(t,
		stor,
		labels.FromStrings(labels.MetricName, "http_requests_total", "job", "api", "code", "500"),
		1, now,
		labels.FromStrings("trace_id", "xyz"), 0.25, now,
	)

	psConfig := &proxyconfig.Config{}
	if err := yaml.Unmarshal([]byte(fmt.Sprintf(`
promxy:
  http_client:
    tls_config:
      insecure_skip_verify: true
  server_groups:
    - static_configs:
        - targets:
          - %s
`, addr)), &psConfig); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}

	ps, err := proxystorage.NewProxyStorage(func(int64) int64 { return 60000 }, "")
	if err != nil {
		t.Fatalf("NewProxyStorage: %v", err)
	}
	if err := ps.ApplyConfig(psConfig); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	eq, err := ps.ExemplarQuerier(context.Background())
	if err != nil {
		t.Fatalf("ExemplarQuerier: %v", err)
	}

	t.Run("specific selector hits one series", func(t *testing.T) {
		got, err := eq.Select(now-5000, now+5000, []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "http_requests_total"),
			labels.MustNewMatcher(labels.MatchEqual, "code", "200"),
		})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1 series, got %d (%+v)", len(got), got)
		}
		if got[0].SeriesLabels.Get("code") != "200" {
			t.Fatalf("wrong series: %v", got[0].SeriesLabels)
		}
		if len(got[0].Exemplars) != 1 || got[0].Exemplars[0].Labels.Get("trace_id") != "abc" {
			t.Fatalf("wrong exemplar: %+v", got[0].Exemplars)
		}
	})

	t.Run("regex selector hits both series", func(t *testing.T) {
		got, err := eq.Select(now-5000, now+5000, []*labels.Matcher{
			labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "http_requests_total"),
			labels.MustNewMatcher(labels.MatchRegexp, "code", "200|500"),
		})
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2 series, got %d (%+v)", len(got), got)
		}
		sort.Slice(got, func(i, j int) bool { return got[i].SeriesLabels.Get("code") < got[j].SeriesLabels.Get("code") })
		traces := []string{
			got[0].Exemplars[0].Labels.Get("trace_id"),
			got[1].Exemplars[0].Labels.Get("trace_id"),
		}
		want := []string{"abc", "xyz"}
		if traces[0] != want[0] || traces[1] != want[1] {
			t.Fatalf("trace mismatch: want %v got %v", want, traces)
		}
	})

	t.Run("multiple selectors merge to deduplicated series", func(t *testing.T) {
		// Both selectors target the same series — Select should emit one
		// merged result, not two duplicates.
		got, err := eq.Select(now-5000, now+5000,
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "http_requests_total"),
				labels.MustNewMatcher(labels.MatchEqual, "code", "200"),
			},
			[]*labels.Matcher{
				labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "http_requests_total"),
				labels.MustNewMatcher(labels.MatchEqual, "job", "api"),
				labels.MustNewMatcher(labels.MatchEqual, "code", "200"),
			},
		)
		if err != nil {
			t.Fatalf("Select: %v", err)
		}
		count := 0
		for _, qr := range got {
			if qr.SeriesLabels.Get("code") == "200" {
				count++
			}
		}
		if count != 1 {
			t.Fatalf("expected 1 merged code=200 series, got %d (%+v)", count, got)
		}
	})

	// Sanity: the underlying storage really has both exemplars, so any
	// failure above is in promxy's path, not the data.
	if got := exemplarCount(t, stor); got != 2 {
		t.Fatalf("test storage should hold 2 exemplars, got %d", got)
	}
}

func exemplarCount(t *testing.T, stor *teststorage.TestStorage) int {
	t.Helper()
	q, err := stor.ExemplarQueryable().ExemplarQuerier(context.Background())
	if err != nil {
		t.Fatalf("ExemplarQuerier: %v", err)
	}
	res, err := q.Select(0, time.Now().Unix()*1000+10000,
		[]*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, labels.MetricName, "http_requests_total")},
	)
	if err != nil {
		t.Fatalf("Select: %v", err)
	}
	n := 0
	for _, r := range res {
		n += len(r.Exemplars)
	}
	return n
}
