package proxystorage

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/tsdb/chunkenc"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

// TestReplicaSetDeduplication verifies that server groups sharing a replica_set
// are deduplicated using anti_affinity, preventing sample interleaving.
func TestReplicaSetDeduplication(t *testing.T) {
	oldInterval := servergroup.DiscoveryUpdateInterval
	servergroup.DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { servergroup.DiscoveryUpdateInterval = oldInterval }()

	// Server A returns samples at t=100 and t=120
	srvA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"http_requests_total"},"values":[[100.000,"10"],[120.000,"20"]]}` +
			`]}}`))
	}))
	defer srvA.Close()

	// Server B returns slightly offset samples (1s jitter) at t=101 and t=121
	srvB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"http_requests_total"},"values":[[101.000,"10"],[121.000,"20"]]}` +
			`]}}`))
	}))
	defer srvB.Close()

	hostA := strings.TrimPrefix(srvA.URL, "http://")
	hostB := strings.TrimPrefix(srvB.URL, "http://")

	// Case 1: With replica_set configured
	t.Run("with replica_set deduplicates samples", func(t *testing.T) {
		cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - replica_set: eu
      anti_affinity: 10s
      labels:
        region: eu
      static_configs:
        - targets: [%q]
    - replica_set: eu
      anti_affinity: 10s
      labels:
        region: eu
      static_configs:
        - targets: [%q]
`, hostA, hostB)

		cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
		if err != nil {
			t.Fatalf("ConfigFromBytes: %v", err)
		}

		ps := &ProxyStorage{}
		if err := ps.ApplyConfig(cfg); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		defer ps.GetState().Cancel(nil)
		ps.GetState().Ready()

		q, err := ps.Querier(0, 200000)
		if err != nil {
			t.Fatalf("Querier: %v", err)
		}
		defer q.Close()

		matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "http_requests_total")
		ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
		if ss.Err() != nil {
			t.Fatalf("Select error: %v", ss.Err())
		}

		seriesCount := 0
		sampleCount := 0
		for ss.Next() {
			seriesCount++
			it := ss.At().Iterator(nil)
			for it.Next() != chunkenc.ValNone {
				sampleCount++
			}
			if it.Err() != nil {
				t.Fatalf("Iterator error: %v", it.Err())
			}
		}

		if seriesCount != 1 {
			t.Fatalf("expected 1 series, got %d", seriesCount)
		}
		// With anti-affinity (10s), t=101 is dropped in favor of t=100, and t=121 is dropped in favor of t=120
		if sampleCount != 2 {
			t.Fatalf("expected 2 deduplicated samples, got %d", sampleCount)
		}
	})

	// Case 2: Without replica_set, samples are interleaved across groups
	t.Run("without replica_set samples interleave", func(t *testing.T) {
		cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - labels:
        region: eu
      static_configs:
        - targets: [%q]
    - labels:
        region: eu
      static_configs:
        - targets: [%q]
`, hostA, hostB)

		cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
		if err != nil {
			t.Fatalf("ConfigFromBytes: %v", err)
		}

		ps := &ProxyStorage{}
		if err := ps.ApplyConfig(cfg); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		defer ps.GetState().Cancel(nil)
		ps.GetState().Ready()

		q, err := ps.Querier(0, 200000)
		if err != nil {
			t.Fatalf("Querier: %v", err)
		}
		defer q.Close()

		matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "http_requests_total")
		ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
		if ss.Err() != nil {
			t.Fatalf("Select error: %v", ss.Err())
		}

		sampleCount := 0
		for ss.Next() {
			it := ss.At().Iterator(nil)
			for it.Next() != chunkenc.ValNone {
				sampleCount++
			}
		}

		// Cross-group merge with anti_affinity=0 retains all 4 samples interleaved
		if sampleCount != 4 {
			t.Fatalf("expected 4 interleaved samples without replica_set, got %d", sampleCount)
		}
	})
}

// TestReplicaSetFailover verifies that losing one replica in a replica_set allows
// queries to succeed from the healthy replica, while without replica_set losing one fails.
func TestReplicaSetFailover(t *testing.T) {
	oldInterval := servergroup.DiscoveryUpdateInterval
	servergroup.DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { servergroup.DiscoveryUpdateInterval = oldInterval }()

	srvHealthy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"up"},"values":[[100.000,"1"]]}` +
			`]}}`))
	}))
	defer srvHealthy.Close()

	var failCount atomic.Int64
	srvFailing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		failCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","errorType":"internal","error":"backend down"}`))
	}))
	defer srvFailing.Close()

	hostHealthy := strings.TrimPrefix(srvHealthy.URL, "http://")
	hostFailing := strings.TrimPrefix(srvFailing.URL, "http://")

	t.Run("with replica_set one down still answers query", func(t *testing.T) {
		cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
`, hostHealthy, hostFailing)

		cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
		if err != nil {
			t.Fatalf("ConfigFromBytes: %v", err)
		}

		ps := &ProxyStorage{}
		if err := ps.ApplyConfig(cfg); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		defer ps.GetState().Cancel(nil)
		ps.GetState().Ready()

		q, err := ps.Querier(0, 200000)
		if err != nil {
			t.Fatalf("Querier: %v", err)
		}
		defer q.Close()

		matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "up")
		ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
		if ss.Err() != nil {
			t.Fatalf("expected query to succeed with 1 replica down, got error: %v", ss.Err())
		}

		count := 0
		for ss.Next() {
			count++
		}
		if count != 1 {
			t.Fatalf("expected 1 series answered, got %d", count)
		}
	})

	t.Run("without replica_set one down fails query", func(t *testing.T) {
		cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - labels:
        region: eu
      static_configs:
        - targets: [%q]
    - labels:
        region: eu
      static_configs:
        - targets: [%q]
`, hostHealthy, hostFailing)

		cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
		if err != nil {
			t.Fatalf("ConfigFromBytes: %v", err)
		}

		ps := &ProxyStorage{}
		if err := ps.ApplyConfig(cfg); err != nil {
			t.Fatalf("ApplyConfig: %v", err)
		}
		defer ps.GetState().Cancel(nil)
		ps.GetState().Ready()

		q, err := ps.Querier(0, 200000)
		if err != nil {
			t.Fatalf("Querier: %v", err)
		}
		defer q.Close()

		matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "up")
		ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
		if ss.Err() == nil {
			t.Fatalf("expected query to fail when plain server group is down, but succeeded")
		}
	})
}

// TestReplicaSetBothDownFails verifies that when every replica in a replica_set fails,
// the query fails loudly instead of returning an empty success.
func TestReplicaSetBothDownFails(t *testing.T) {
	oldInterval := servergroup.DiscoveryUpdateInterval
	servergroup.DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { servergroup.DiscoveryUpdateInterval = oldInterval }()

	srvFail1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","errorType":"internal","error":"replica 1 down"}`))
	}))
	defer srvFail1.Close()

	srvFail2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","errorType":"internal","error":"replica 2 down"}`))
	}))
	defer srvFail2.Close()

	host1 := strings.TrimPrefix(srvFail1.URL, "http://")
	host2 := strings.TrimPrefix(srvFail2.URL, "http://")

	cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
`, host1, host2)

	cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
	if err != nil {
		t.Fatalf("ConfigFromBytes: %v", err)
	}

	ps := &ProxyStorage{}
	if err := ps.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	defer ps.GetState().Cancel(nil)
	ps.GetState().Ready()

	q, err := ps.Querier(0, 200000)
	if err != nil {
		t.Fatalf("Querier: %v", err)
	}
	defer q.Close()

	matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "up")
	ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
	if ss.Err() == nil {
		t.Fatalf("expected query to fail when all replicas are down, but it succeeded")
	}
}

// TestReplicaSetMixedWithStandalone verifies that losing one replica in a replica_set
// does not fail the top-level query when other standalone groups are present, but losing
// a standalone group does fail the query.
func TestReplicaSetMixedWithStandalone(t *testing.T) {
	oldInterval := servergroup.DiscoveryUpdateInterval
	servergroup.DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { servergroup.DiscoveryUpdateInterval = oldInterval }()

	srvEuA := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"up","region":"eu"},"values":[[100.000,"1"]]}` +
			`]}}`))
	}))
	defer srvEuA.Close()

	srvEuB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"status":"error","errorType":"internal","error":"eu replica B down"}`))
	}))
	defer srvEuB.Close()

	var usHealthy atomic.Bool
	usHealthy.Store(true)
	srvUs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !usHealthy.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"status":"error","errorType":"internal","error":"us down"}`))
			return
		}
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
			`{"metric":{"__name__":"up","region":"us"},"values":[[100.000,"2"]]}` +
			`]}}`))
	}))
	defer srvUs.Close()

	hostEuA := strings.TrimPrefix(srvEuA.URL, "http://")
	hostEuB := strings.TrimPrefix(srvEuB.URL, "http://")
	hostUs := strings.TrimPrefix(srvUs.URL, "http://")

	cfgYAML := fmt.Sprintf(`
promxy:
  server_groups:
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
    - replica_set: eu
      labels:
        region: eu
      static_configs:
        - targets: [%q]
    - labels:
        region: us
      static_configs:
        - targets: [%q]
`, hostEuA, hostEuB, hostUs)

	cfg, err := proxyconfig.ConfigFromBytes([]byte(cfgYAML))
	if err != nil {
		t.Fatalf("ConfigFromBytes: %v", err)
	}

	ps := &ProxyStorage{}
	if err := ps.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	defer ps.GetState().Cancel(nil)
	ps.GetState().Ready()

	q, err := ps.Querier(0, 200000)
	if err != nil {
		t.Fatalf("Querier: %v", err)
	}
	defer q.Close()

	matcher, _ := labels.NewMatcher(labels.MatchEqual, "__name__", "up")

	// euB is down, but euA and us are up -> query succeeds and returns both eu and us series
	ss := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
	if ss.Err() != nil {
		t.Fatalf("expected query to succeed with euB down, got error: %v", ss.Err())
	}
	seriesCount := 0
	for ss.Next() {
		seriesCount++
	}
	if seriesCount != 2 {
		t.Fatalf("expected 2 series (eu and us), got %d", seriesCount)
	}

	// Now make standalone us fail -> query must fail
	usHealthy.Store(false)
	ssFail := q.Select(context.Background(), true, &storage.SelectHints{Start: 0, End: 200000, Step: 10000}, matcher)
	if ssFail.Err() == nil {
		t.Fatalf("expected query to fail when required standalone group is down, but succeeded")
	}
}
