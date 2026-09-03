package promclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/prometheus/common/model"
)

func TestHealthCheckConfigValidate(t *testing.T) {
	// Defaults are filled in.
	c := &HealthCheckConfig{}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Path != defaultHealthCheckPath {
		t.Fatalf("expected default path %q, got %q", defaultHealthCheckPath, c.Path)
	}
	if c.Interval != defaultHealthCheckInterval {
		t.Fatalf("expected default interval %s, got %s", defaultHealthCheckInterval, c.Interval)
	}
	if c.Timeout != defaultHealthCheckTimeout {
		t.Fatalf("expected default timeout %s, got %s", defaultHealthCheckTimeout, c.Timeout)
	}
	if c.FailureThreshold != defaultHealthCheckThreshold {
		t.Fatalf("expected default failure_threshold %d, got %d", defaultHealthCheckThreshold, c.FailureThreshold)
	}
	if c.SuccessThreshold != defaultHealthCheckThreshold {
		t.Fatalf("expected default success_threshold %d, got %d", defaultHealthCheckThreshold, c.SuccessThreshold)
	}

	// Explicit values are preserved.
	c = &HealthCheckConfig{Path: "/healthz", Interval: time.Minute, Timeout: time.Second, FailureThreshold: 3, SuccessThreshold: 2}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
	if c.Path != "/healthz" || c.Interval != time.Minute || c.Timeout != time.Second || c.FailureThreshold != 3 || c.SuccessThreshold != 2 {
		t.Fatalf("expected explicit config to be preserved, got %+v", c)
	}

	// timeout must be less than interval.
	c = &HealthCheckConfig{Interval: time.Second, Timeout: time.Second}
	if err := c.Validate(); err == nil {
		t.Fatal("expected error when timeout >= interval")
	}
}

// statusHandler is a toggleable httptest handler: it replies with the status
// code currently stored in code, or hangs past hangFor (if non-zero) before
// ever writing a response -- simulating "TCP connects but HTTP never answers".
type statusHandler struct {
	code    atomic.Int32
	hangFor atomic.Int64 // time.Duration, 0 == don't hang
}

func newStatusHandler(initialCode int) *statusHandler {
	h := &statusHandler{}
	h.code.Store(int32(initialCode))
	return h
}

func (h *statusHandler) setCode(code int) { h.code.Store(int32(code)) }
func (h *statusHandler) setHang(d time.Duration) { h.hangFor.Store(int64(d)) }

func (h *statusHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if d := time.Duration(h.hangFor.Load()); d > 0 {
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
	}
	w.WriteHeader(int(h.code.Load()))
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition not met within %s", timeout)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestHealthCheckClient_HealthyBeforeFirstProbe(t *testing.T) {
	handler := newStatusHandler(http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Interval longer than the test so the first probe never fires.
	cfg := &HealthCheckConfig{Interval: time.Hour, Timeout: time.Second}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())

	if !c.Healthy() {
		t.Fatal("expected target to be healthy before the first probe runs")
	}
	if err := c.Query(ctx, "up", time.Now()).Err(); err != nil {
		t.Fatal(err)
	}
	if underlying.callCount["Query"] != 1 {
		t.Fatalf("expected query to pass through before first probe, got callCount=%d", underlying.callCount["Query"])
	}
}

func TestHealthCheckClient_MarksUnhealthyAfterFailureThreshold(t *testing.T) {
	handler := newStatusHandler(http.StatusOK)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 2, SuccessThreshold: 2}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())

	waitFor(t, time.Second, c.Healthy)

	// Start failing; after 2 consecutive failed probes it should flip unhealthy.
	handler.setCode(http.StatusServiceUnavailable)
	waitFor(t, 2*time.Second, func() bool { return !c.Healthy() })

	before := underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); !errors.Is(err, ErrTargetUnhealthy) {
		t.Fatalf("expected ErrTargetUnhealthy while unhealthy, got: %v", err)
	}
	if got := underlying.callCount["Query"]; got != before {
		t.Fatalf("expected query to be skipped while unhealthy, but downstream was called: before=%d after=%d", before, got)
	}
}

func TestHealthCheckClient_RecoversAfterSuccessThreshold(t *testing.T) {
	handler := newStatusHandler(http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 1, SuccessThreshold: 2}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())

	waitFor(t, time.Second, func() bool { return !c.Healthy() })

	before := underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); !errors.Is(err, ErrTargetUnhealthy) {
		t.Fatalf("expected ErrTargetUnhealthy while unhealthy, got: %v", err)
	}
	if got := underlying.callCount["Query"]; got != before {
		t.Fatalf("expected query to be skipped while unhealthy: before=%d after=%d", before, got)
	}

	handler.setCode(http.StatusOK)
	waitFor(t, time.Second, c.Healthy)

	before = underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); err != nil {
		t.Fatal(err)
	}
	if got := underlying.callCount["Query"]; got != before+1 {
		t.Fatalf("expected query to pass through after recovery: before=%d after=%d", before, got)
	}
}

// TestHealthCheckClient_RecoversAfterRelapse proves a target that flaps
// healthy -> unhealthy -> healthy again is tracked correctly on the second
// transition too, not just the first.
func TestHealthCheckClient_RecoversAfterRelapse(t *testing.T) {
	handler := newStatusHandler(http.StatusOK)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 1, SuccessThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())

	waitFor(t, time.Second, c.Healthy)

	before := underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); err != nil {
		t.Fatal(err)
	}
	if got := underlying.callCount["Query"]; got != before+1 {
		t.Fatalf("expected query to pass through while healthy: before=%d after=%d", before, got)
	}

	// Relapse: the target goes unhealthy again.
	handler.setCode(http.StatusServiceUnavailable)
	waitFor(t, time.Second, func() bool { return !c.Healthy() })

	before = underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); !errors.Is(err, ErrTargetUnhealthy) {
		t.Fatalf("expected ErrTargetUnhealthy after relapse, got: %v", err)
	}
	if got := underlying.callCount["Query"]; got != before {
		t.Fatalf("expected query to be skipped after relapse, but downstream was called: before=%d after=%d", before, got)
	}

	// Recovers again from the relapse.
	handler.setCode(http.StatusOK)
	waitFor(t, time.Second, c.Healthy)

	before = underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); err != nil {
		t.Fatal(err)
	}
	if got := underlying.callCount["Query"]; got != before+1 {
		t.Fatalf("expected query to pass through again after recovering from relapse: before=%d after=%d", before, got)
	}
}

// TestHealthCheckClient_HungTargetDetectedWithinTimeout proves a hung target
// (TCP connects, HTTP never answers) is caught within the probe's own
// timeout, not the query-path timeout.
func TestHealthCheckClient_HungTargetDetectedWithinTimeout(t *testing.T) {
	handler := newStatusHandler(http.StatusOK)
	handler.setHang(time.Hour) // simulate a server that never answers
	srv := httptest.NewServer(handler)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	const probeTimeout = 50 * time.Millisecond
	cfg := &HealthCheckConfig{Interval: 100 * time.Millisecond, Timeout: probeTimeout, FailureThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())

	// The hung target must be detected well within a couple of probe timeouts,
	// not the hour it would take an un-bounded call to fail.
	waitFor(t, 2*time.Second, func() bool { return !c.Healthy() })

	before := underlying.callCount["Query"]
	if err := c.Query(ctx, "up", time.Now()).Err(); !errors.Is(err, ErrTargetUnhealthy) {
		t.Fatalf("expected ErrTargetUnhealthy against a hung target, got: %v", err)
	}
	if got := underlying.callCount["Query"]; got != before {
		t.Fatalf("expected query to be skipped against a hung target: before=%d after=%d", before, got)
	}
}

// newHealthCheckedTarget wraps a countAPI-instrumented stub, serving a single
// sample for any query, in a HealthCheckClient probing an httptest.Server
// controlled by the returned *statusHandler.
func newHealthCheckedTarget(t *testing.T, ctx context.Context, initialCode int) (*HealthCheckClient, *statusHandler, *countAPI) {
	t.Helper()
	handler := newStatusHandler(initialCode)
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	cfg := &HealthCheckConfig{Interval: 20 * time.Millisecond, Timeout: 10 * time.Millisecond, FailureThreshold: 1, SuccessThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	underlying := newCountAPI(&stubAPI{
		query: func() model.Value {
			return model.Vector{
				&model.Sample{Metric: model.Metric{"__name__": "up"}, Value: 1, Timestamp: 0},
			}
		},
	})
	c := NewHealthCheckClient(ctx, underlying, cfg, srv.URL, srv.Client())
	return c, handler, underlying
}

// TestHealthCheckClient_MultiAPIExcludesUnhealthyTarget proves an unhealthy
// target is excluded from MultiAPI's quorum/merge, not counted as a
// successful empty result.
func TestHealthCheckClient_MultiAPIExcludesUnhealthyTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clientA, handlerA, _ := newHealthCheckedTarget(t, ctx, http.StatusOK)
	clientB, handlerB, _ := newHealthCheckedTarget(t, ctx, http.StatusOK)
	handlerB.setCode(http.StatusServiceUnavailable)

	waitFor(t, 2*time.Second, func() bool { return clientA.Healthy() && !clientB.Healthy() })

	multi, err := NewMultiAPI([]API{clientA, clientB}, 0, false, nil, 1, false)
	if err != nil {
		t.Fatal(err)
	}

	if err := multi.Query(ctx, "up", time.Now()).Err(); err != nil {
		t.Fatalf("expected the healthy target alone to satisfy quorum (requiredCount=1), got error: %v", err)
	}

	// Now take the previously-healthy target down too -- with every target in
	// the quorum group unhealthy, MultiAPI must surface a failure, not
	// silently return an empty successful result.
	handlerA.setCode(http.StatusServiceUnavailable)
	waitFor(t, 2*time.Second, func() bool { return !clientA.Healthy() })

	if err := multi.Query(ctx, "up", time.Now()).Err(); err == nil {
		t.Fatal("expected an error once every target in the quorum group is unhealthy, got nil")
	}
}

// erroringLabelValuesAPI always fails LabelValues, so a LabelFilterClient
// wrapping it can never sync -- used to drive it into a permanent blocked()
// state.
type erroringLabelValuesAPI struct {
	*stubAPI
}

func (erroringLabelValuesAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, fmt.Errorf("stub: forced LabelValues failure")
}

// newBlockedLabelFilter builds a LabelFilterClient (on_sync_error: closed)
// whose initial sync always fails, so it stays blocked() for the test.
func newBlockedLabelFilter(t *testing.T, ctx context.Context) (*LabelFilterClient, *countAPI) {
	t.Helper()
	underlying := newCountAPI(erroringLabelValuesAPI{&stubAPI{}})
	lfCfg := &LabelFilterConfig{DynamicLabels: []string{"__name__"}, OnSyncError: LabelFilterOnSyncErrorClosed}
	if err := lfCfg.Validate(); err != nil {
		t.Fatal(err)
	}
	lf, err := NewLabelFilterClient(ctx, underlying, lfCfg)
	if err != nil {
		t.Fatal(err)
	}
	if !lf.blocked() {
		t.Fatal("expected label_filter to be blocked from a failed initial sync")
	}
	return lf, underlying
}

// TestHealthCheckClient_ProbesRegardlessOfLabelFilterBlocked proves the probe
// loop still runs and flips healthy/unhealthy even when the wrapped
// LabelFilterClient is permanently blocked().
func TestHealthCheckClient_ProbesRegardlessOfLabelFilterBlocked(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lf, _ := newBlockedLabelFilter(t, ctx)

	handler := newStatusHandler(http.StatusOK)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 1, SuccessThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	c := NewHealthCheckClient(ctx, lf, cfg, srv.URL, srv.Client())

	waitFor(t, time.Second, c.Healthy)
	if !lf.blocked() {
		t.Fatal("expected label_filter to still be blocked")
	}

	handler.setCode(http.StatusServiceUnavailable)
	waitFor(t, time.Second, func() bool { return !c.Healthy() })
}

// TestHealthCheckClient_UnhealthyShortCircuitsBeforeLabelFilter proves that
// while unhealthy, calls never reach the wrapped LabelFilterClient at all --
// filteredCount staying flat rules out falling through to its own blocked()
// check.
func TestHealthCheckClient_UnhealthyShortCircuitsBeforeLabelFilter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	lf, underlying := newBlockedLabelFilter(t, ctx)

	handler := newStatusHandler(http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	c := NewHealthCheckClient(ctx, lf, cfg, srv.URL, srv.Client())

	waitFor(t, 2*time.Second, func() bool { return !c.Healthy() })

	before := testutil.ToFloat64(filteredCount.WithLabelValues("Query"))
	if err := c.Query(ctx, "up", time.Now()).Err(); !errors.Is(err, ErrTargetUnhealthy) {
		t.Fatalf("expected ErrTargetUnhealthy, got: %v", err)
	}
	after := testutil.ToFloat64(filteredCount.WithLabelValues("Query"))
	if after != before {
		t.Fatalf("expected LabelFilterClient.Query to never be invoked (filteredCount unchanged): before=%v after=%v", before, after)
	}
	if got := underlying.callCount["Query"]; got != 0 {
		t.Fatalf("expected downstream to never be called, got callCount=%d", got)
	}
}

// TestHealthCheckClient_RecoveryTriggersLabelFilterSync proves recovery
// triggers an immediate LabelFilterClient.Sync rather than waiting for its
// (deliberately very long) sync_interval.
func TestHealthCheckClient_RecoveryTriggersLabelFilterSync(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	underlying := newCountAPI(&stubAPI{
		labelValues: func(label string) model.LabelValues { return model.LabelValues{"up"} },
	})
	lfCfg := &LabelFilterConfig{DynamicLabels: []string{"__name__"}, SyncInterval: time.Hour}
	if err := lfCfg.Validate(); err != nil {
		t.Fatal(err)
	}
	lf, err := NewLabelFilterClient(ctx, underlying, lfCfg)
	if err != nil {
		t.Fatal(err)
	}
	baseline := underlying.count("LabelValues")

	handler := newStatusHandler(http.StatusServiceUnavailable)
	srv := httptest.NewServer(handler)
	defer srv.Close()

	cfg := &HealthCheckConfig{Interval: 10 * time.Millisecond, Timeout: 5 * time.Millisecond, FailureThreshold: 1, SuccessThreshold: 1}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	c := NewHealthCheckClient(ctx, lf, cfg, srv.URL, srv.Client())

	waitFor(t, time.Second, func() bool { return !c.Healthy() })

	handler.setCode(http.StatusOK)
	waitFor(t, time.Second, c.Healthy)

	waitFor(t, time.Second, func() bool { return underlying.count("LabelValues") > baseline })
}
