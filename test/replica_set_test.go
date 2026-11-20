package test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/util/teststorage"
)

// replicaCopy is one copy of the data: a real Prometheus API in front of its
// own storage, reachable only through a gateway that demands its own
// basic_auth credentials (as a tenant of a multi-tenant backend would).
type replicaCopy struct {
	gateway    *httptest.Server
	authorized atomic.Int64
	rejected   atomic.Int64
}

func (c *replicaCopy) addr() string { return strings.TrimPrefix(c.gateway.URL, "http://") }

// startReplicaCopy stores up{job="node"}=1 every 15s for 10m, starting at
// offsetSec, so two copies started with different offsets never share a
// timestamp, the way two independently scraped copies don't.
func startReplicaCopy(t *testing.T, user, pass string, offsetSec int64) *replicaCopy {
	t.Helper()
	stor := teststorage.New(t)
	app := stor.Appender(context.Background())
	lset := labels.FromStrings("__name__", "up", "job", "node")
	for ts := offsetSec; ts <= 600; ts += 15 {
		if _, err := app.Append(0, lset, ts*1000, 1); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.Commit(); err != nil {
		t.Fatal(err)
	}

	srv, addr, stop := startAPIForTest(stor)
	t.Cleanup(func() { srv.Shutdown(context.Background()); <-stop; stor.Close() })

	backend, _ := url.Parse("http://" + addr)
	proxy := httputil.NewSingleHostReverseProxy(backend)
	c := &replicaCopy{}
	c.gateway = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, p, ok := r.BasicAuth(); !ok || u != user || p != pass {
			c.rejected.Add(1)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		c.authorized.Add(1)
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(c.gateway.Close)
	return c
}

// TestReplicaSet runs promxy in front of two copies of the same data that
// need different credentials, so each sits in its own server group (#842).
func TestReplicaSet(t *testing.T) {
	serverGroup := func(c *replicaCopy, user, pass, replicaSet string, noTargets bool) string {
		target := c.addr()
		if noTargets {
			target = ""
		}
		s := fmt.Sprintf(`
    - static_configs:
        - targets: [%s]
      labels: {region: eu}
      anti_affinity: 20s
      http_client:
        basic_auth: {username: %s, password: %s}
`, target, user, pass)
		if replicaSet != "" {
			s += "      replica_set: " + replicaSet + "\n"
		}
		return s
	}

	// up[1m] at t=300 holds 4 samples per copy (15s scrape interval).
	const query = "/api/v1/query?query=up[1m]&time=300"

	cases := []struct {
		name       string
		replicaSet string
		down       []bool // which copies are unreachable
		noTargets  bool   // the first copy's server group discovers no targets
		wantErr    bool
		wantPoints int
	}{
		// Without replica_set the copies are distinct server groups: their
		// samples interleave, and both are required.
		{name: "no_set/both_up", wantPoints: 8},
		{name: "no_set/one_down", down: []bool{true, false}, wantErr: true},

		{name: "set/both_up", replicaSet: "eu", wantPoints: 4},
		{name: "set/first_down", replicaSet: "eu", down: []bool{true, false}, wantPoints: 4},
		{name: "set/second_down", replicaSet: "eu", down: []bool{false, true}, wantPoints: 4},
		{name: "set/both_down", replicaSet: "eu", down: []bool{true, true}, wantErr: true},

		// A member with no targets would otherwise answer with an empty
		// success, standing in for the member that holds the data.
		{name: "set/no_targets_other_up", replicaSet: "eu", noTargets: true, wantPoints: 4},
		{name: "set/no_targets_other_down", replicaSet: "eu", noTargets: true, down: []bool{false, true}, wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			copies := []*replicaCopy{
				startReplicaCopy(t, "tenant-a", "secret-a", 0),
				startReplicaCopy(t, "tenant-b", "secret-b", 5),
			}
			cfg := "promxy:\n  server_groups:\n" +
				serverGroup(copies[0], "tenant-a", "secret-a", tc.replicaSet, tc.noTargets) +
				serverGroup(copies[1], "tenant-b", "secret-b", tc.replicaSet, false)
			for i, down := range tc.down {
				if down {
					copies[i].gateway.Close()
				}
			}

			ps := getProxyStorage(cfg)
			defer ps.GetState().Cancel(nil)
			srv, addr, stop := startAPIForTest(ps)
			defer func() { srv.Shutdown(context.Background()); <-stop }()

			resp, err := http.Get("http://" + addr + query)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			var got struct {
				Status string `json:"status"`
				Error  string `json:"error"`
				Data   struct {
					Result []struct {
						Metric map[string]string `json:"metric"`
						Values [][2]interface{}  `json:"values"`
					} `json:"result"`
				} `json:"data"`
			}
			if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
				t.Fatal(err)
			}

			if tc.wantErr {
				if got.Status != "error" {
					t.Fatalf("query succeeded with every required copy down: %+v", got.Data)
				}
				return
			}
			if got.Status != "success" {
				t.Fatalf("query failed: %s", got.Error)
			}
			if len(got.Data.Result) != 1 {
				t.Fatalf("got %d series, want 1: %+v", len(got.Data.Result), got.Data.Result)
			}
			if n := len(got.Data.Result[0].Values); n != tc.wantPoints {
				t.Errorf("got %d samples, want %d: %v", n, tc.wantPoints, got.Data.Result[0].Values)
			}
			if region := got.Data.Result[0].Metric["region"]; region != "eu" {
				t.Errorf("region label = %q, want eu", region)
			}

			for i, c := range copies {
				if c.rejected.Load() != 0 {
					t.Errorf("copy %d rejected %d requests with the wrong credentials", i, c.rejected.Load())
				}
				up := (len(tc.down) == 0 || !tc.down[i]) && !(i == 0 && tc.noTargets)
				if up && c.authorized.Load() == 0 {
					t.Errorf("copy %d is up but was never queried", i)
				}
			}
		})
	}
}
