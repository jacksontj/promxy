package servergroup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"gopkg.in/yaml.v2"

	"github.com/jacksontj/promxy/pkg/middleware"
)

// TestRemoteReadForwardsHeaders checks that remote_read requests carry the
// --proxy-headers forwarded from the client request and the server group's
// http_headers, like every other downstream request (#851), and that each
// header is sent once even when several sources set it.
func TestRemoteReadForwardsHeaders(t *testing.T) {
	oldInterval := DiscoveryUpdateInterval
	DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { DiscoveryUpdateInterval = oldInterval }()

	tests := []struct {
		name string
		// config is appended to the server group config.
		config   string
		incoming map[string]string
		want     map[string][]string
	}{
		{
			name: "forwarded and http_headers",
			config: `
http_headers:
  X-Example: configured-value
  X-Scope-OrgID: configured-tenant
`,
			incoming: map[string]string{"Authorization": "Bearer example-token", "X-Scope-OrgID": "tenant-a"},
			want: map[string][]string{
				"Authorization": {"Bearer example-token"},
				"X-Example":     {"configured-value"},
				// http_headers wins over a forwarded header of the same name.
				"X-Scope-OrgID": {"configured-tenant"},
			},
		},
		{
			name: "http_headers wins over http_client.http_headers",
			config: `
http_headers:
  x-scope-orgid: sg-tenant
http_client:
  http_headers:
    X-Scope-OrgID:
      values: [cc-tenant]
    X-Other:
      values: [cc-other]
`,
			incoming: map[string]string{"X-Scope-OrgID": "tenant-a"},
			want: map[string][]string{
				"X-Scope-OrgID": {"sg-tenant"},
				"X-Other":       {"cc-other"},
			},
		},
		{
			name: "http_client.http_headers wins over forwarded",
			config: `
http_client:
  http_headers:
    X-Scope-OrgID:
      values: [cc-tenant]
`,
			incoming: map[string]string{"X-Scope-OrgID": "tenant-a", "Authorization": "Bearer example-token"},
			want: map[string][]string{
				"X-Scope-OrgID": {"cc-tenant"},
				"Authorization": {"Bearer example-token"},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := remoteReadRequestHeaders(t, tc.config, tc.incoming)
			for k, want := range tc.want {
				if v := got.Values(k); !reflect.DeepEqual(v, want) {
					t.Errorf("remote_read header %s = %q, want %q", k, v, want)
				}
			}
		})
	}
}

// remoteReadRequestHeaders runs a remote_read through a server group built from
// extraConfig, for a client request carrying incoming (all forwarded via
// --proxy-headers), and returns the headers the downstream received.
func remoteReadRequestHeaders(t *testing.T, extraConfig string, incoming map[string]string) http.Header {
	t.Helper()
	var (
		mu  sync.Mutex
		got http.Header
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/prometheus/api/v1/read" {
			mu.Lock()
			got = r.Header.Clone()
			mu.Unlock()
		}
		http.Error(w, "not implemented", http.StatusNotImplemented)
	}))
	defer srv.Close()

	cfg := &Config{}
	raw := `
scheme: http
path_prefix: /prometheus
remote_read: true
static_configs:
  - targets: [` + strings.TrimPrefix(srv.URL, "http://") + `]
` + extraConfig
	if err := yaml.Unmarshal([]byte(raw), cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	sg, err := NewServerGroup()
	if err != nil {
		t.Fatalf("NewServerGroup: %v", err)
	}
	defer sg.Cancel()
	if err := sg.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	select {
	case <-sg.Ready:
	case <-time.After(10 * time.Second):
		t.Fatal("server group never became ready")
	}

	// Build the request context the way promxy's HTTP server does.
	var ctx context.Context
	names := make([]string, 0, len(incoming))
	in := httptest.NewRequest(http.MethodGet, "/api/v1/query", nil)
	for k, v := range incoming {
		names = append(names, k)
		in.Header.Set(k, v)
	}
	h := middleware.NewProxyHeaders(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		ctx = r.Context()
	}), names)
	h.ServeHTTP(httptest.NewRecorder(), in)

	end := time.Now()
	ss := sg.GetValue(ctx, end.Add(-5*time.Minute), end, []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "__name__", "up")})
	for ss.Next() {
	}

	mu.Lock()
	defer mu.Unlock()
	if got == nil {
		t.Fatalf("no remote_read request reached the downstream (err: %v)", ss.Err())
	}
	return got
}
