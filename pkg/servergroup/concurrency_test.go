package servergroup

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

// TestConfigAccessorLifecycle covers the contract of the Config() accessor:
// nil before the first ApplyConfig (callers such as groupIdentifier and the
// proxystorage histogram routing rely on that), and the applied config after.
func TestConfigAccessorLifecycle(t *testing.T) {
	sg, err := NewServerGroup()
	if err != nil {
		t.Fatalf("NewServerGroup: %v", err)
	}
	defer sg.Cancel()

	if got := sg.Config(); got != nil {
		t.Fatalf("Config() before ApplyConfig = %v, want nil", got)
	}
	// The nil config must not panic anything that reads it.
	if got := sg.groupIdentifier(); got != "unknown" {
		t.Fatalf("groupIdentifier() before ApplyConfig = %q, want %q", got, "unknown")
	}

	cfg := &Config{Ordinal: 7, Name: "accessor-test", Scheme: "http"}
	if err := sg.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	if got := sg.Config(); got != cfg {
		t.Fatalf("Config() after ApplyConfig = %v, want %v", got, cfg)
	}
	if got := sg.groupIdentifier(); got != "ord=7 name=accessor-test" {
		t.Fatalf("groupIdentifier() after ApplyConfig = %q", got)
	}
	if sg.httpClient() == nil {
		t.Fatal("httpClient() after ApplyConfig = nil, want a client")
	}
}

// TestApplyConfigConcurrentWithReaders exercises the three concurrent users of
// the ServerGroup config/client: ApplyConfig publishing new ones, RoundTrip
// reading both on every downstream request, and the Sync goroutine reading the
// config while it rebuilds target clients. Run under -race this fails if either
// field is published without synchronization.
func TestApplyConfigConcurrentWithReaders(t *testing.T) {
	// Make service discovery churn quickly so the Sync goroutine actually
	// reloads targets (and thus reads the config) during the test.
	oldInterval := DiscoveryUpdateInterval
	DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { DiscoveryUpdateInterval = oldInterval }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	// Two configs that differ in every field the concurrent readers touch: the
	// custom headers (RoundTrip), and the labels/targets (Sync).
	configs := make([]*Config, 2)
	for i := range configs {
		raw := fmt.Sprintf(`
scheme: http
http_client:
  dial_timeout: 200ms
http_headers:
  X-Promxy-Test: variant-%d
labels:
  variant: "%d"
static_configs:
  - targets: [%q, %q]
`, i, i, host, fmt.Sprintf("127.0.0.%d:9090", i+2))
		cfg := &Config{}
		if err := yaml.Unmarshal([]byte(raw), cfg); err != nil {
			t.Fatalf("unmarshal config %d: %v", i, err)
		}
		cfg.Ordinal = i
		configs[i] = cfg
	}

	sg, err := NewServerGroup()
	if err != nil {
		t.Fatalf("NewServerGroup: %v", err)
	}
	defer sg.Cancel()

	if err := sg.ApplyConfig(configs[0]); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup

	// Readers: hammer the transport-level read path (config headers + client).
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				req, err := http.NewRequest(http.MethodGet, srv.URL, nil)
				if err != nil {
					t.Errorf("NewRequest: %v", err)
					return
				}
				resp, err := sg.RoundTrip(req)
				if err != nil {
					t.Errorf("RoundTrip: %v", err)
					return
				}
				resp.Body.Close()
			}
		}()
	}

	// Writer: reconfigure the live server group. This also re-drives service
	// discovery, so the Sync goroutine reloads targets (reading the config)
	// while these writes are happening.
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(stop)
		for i := 0; i < 200; i++ {
			if err := sg.ApplyConfig(configs[i%len(configs)]); err != nil {
				t.Errorf("ApplyConfig: %v", err)
				return
			}
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Sanity check that the sync path really ran (i.e. the config reads in
	// loadTargetGroupMap were genuinely exercised, not skipped).
	select {
	case <-sg.Ready:
	case <-time.After(10 * time.Second):
		t.Fatal("server group never became ready; target sync never ran")
	}

	// Discovery churn means an update can transiently carry zero targets, so
	// wait for the post-churn state to settle rather than sampling once.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if state := sg.State(); state != nil && len(state.Targets) > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("expected discovered targets after sync, got none")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
