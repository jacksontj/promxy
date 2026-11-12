package servergroup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"gopkg.in/yaml.v2"
)

func requestMetricCount(t *testing.T, host, call string) uint64 {
	t.Helper()
	var total uint64
	for _, status := range []string{"success", "error"} {
		m := &dto.Metric{}
		if err := serverGroupSummary.WithLabelValues(host, call, status).(prometheus.Metric).Write(m); err != nil {
			t.Fatalf("write metric: %v", err)
		}
		total += m.GetSummary().GetSampleCount()
	}
	return total
}

// TestServerGroupRequestMetricCountsDownstreamRequests checks that
// server_group_request_duration_seconds only observes requests actually sent
// to the target, and not calls answered locally by the time range filter.
func TestServerGroupRequestMetricCountsDownstreamRequests(t *testing.T) {
	oldInterval := DiscoveryUpdateInterval
	DiscoveryUpdateInterval = 10 * time.Millisecond
	defer func() { DiscoveryUpdateInterval = oldInterval }()

	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()
	host := strings.TrimPrefix(srv.URL, "http://")

	cfg := &Config{}
	raw := `
scheme: http
relative_time_range:
  start: -1h
static_configs:
  - targets: [` + host + `]
`
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

	now := time.Now()

	// Out of range: answered by the time filter, never sent downstream.
	outOfRange := v1.Range{Start: now.Add(-3 * time.Hour), End: now.Add(-2 * time.Hour), Step: time.Minute}
	if err := sg.QueryRange(context.TODO(), "up", outOfRange).Err(); err != nil {
		t.Fatalf("out of range query_range: %v", err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("expected no downstream requests, got %d", got)
	}
	if got := requestMetricCount(t, host, "query_range"); got != 0 {
		t.Fatalf("expected no observations for a filtered call, got %d", got)
	}

	// In range: sent downstream and observed once.
	inRange := v1.Range{Start: now.Add(-30 * time.Minute), End: now, Step: time.Minute}
	if err := sg.QueryRange(context.TODO(), "up", inRange).Err(); err != nil {
		t.Fatalf("in range query_range: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("expected 1 downstream request, got %d", got)
	}
	if got := requestMetricCount(t, host, "query_range"); got != 1 {
		t.Fatalf("expected 1 observation, got %d", got)
	}
}
