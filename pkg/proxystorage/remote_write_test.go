package proxystorage

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"go.uber.org/goleak"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/servergroup"
)

func noStepSubqueryInterval(int64) int64 { return 0 }

func loadConfig(t *testing.T, raw string) *proxyconfig.Config {
	t.Helper()
	cfg, err := proxyconfig.ConfigFromBytes([]byte(raw))
	if err != nil {
		t.Fatalf("error loading config: %v", err)
	}
	return cfg
}

// TestRemoteWriteWAL is a regression test for
// https://github.com/jacksontj/promxy/issues/771. With remote_write configured,
// promxy must run a WAL-only agent DB so the remote.Storage queue managers have
// a WAL to tail. Before the fix the WAL subdir never existed (the watcher failed
// with "error tailing WAL ... no such file or directory") and samples appended by
// the rule manager were silently discarded by the upstream timestampTracker.
func TestRemoteWriteWAL(t *testing.T) {
	dir := t.TempDir()

	ps, err := NewProxyStorage(noStepSubqueryInterval, dir)
	if err != nil {
		t.Fatalf("NewProxyStorage: %v", err)
	}

	cfg := loadConfig(t, `
remote_write:
  - url: http://localhost:1/api/v1/write
`)

	if err := ps.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	defer ps.GetState().Cancel(nil)

	// The agent DB must have created the WAL subdir that the queue managers tail.
	walDir := filepath.Join(dir, "wal")
	if _, err := os.Stat(walDir); err != nil {
		t.Fatalf("expected WAL dir %q to exist, got: %v", walDir, err)
	}

	// An append+commit must succeed and be accepted by the WAL-backed appender
	// (not silently discarded). The agent appender is single-use, so this also
	// exercises that ProxyStorage.Appender hands out a fresh appender per call.
	app := ps.Appender(context.Background())
	if _, err := app.Append(0, labels.FromStrings(labels.MetricName, "promxy_test_metric"), 1, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// A second Appender call must return a distinct instance; the first one was
	// returned to the agent's pool on Commit. Reusing a shared instance would be
	// a use-after-return bug.
	if a, b := ps.Appender(context.Background()), ps.Appender(context.Background()); a == b {
		t.Fatalf("expected distinct appenders from the WAL-backed appendable, got identical instances")
	}
}

// TestNoRemoteWriteAppender verifies that without remote_write the appendable is
// the no-op stub and appends are accepted (and discarded) without error.
func TestNoRemoteWriteAppender(t *testing.T) {
	ps, err := NewProxyStorage(noStepSubqueryInterval, "")
	if err != nil {
		t.Fatalf("NewProxyStorage: %v", err)
	}

	if err := ps.ApplyConfig(loadConfig(t, "")); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}

	app := ps.Appender(context.Background())
	if _, ok := app.(*appenderStub); !ok {
		t.Fatalf("expected *appenderStub when no remote_write is configured, got %T", app)
	}
	if _, err := app.Append(0, labels.FromStrings(labels.MetricName, "x"), 1, 1); err != nil {
		t.Fatalf("stub Append should not error: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("stub Commit should not error: %v", err)
	}
}

var _ storage.Appendable = appendableStub{}

const (
	rwConfig = `
remote_write:
  - url: http://localhost:1/api/v1/write
promxy:
  server_groups:
    - static_configs:
        - targets: [localhost:1]
`
	noRWConfig = `
promxy:
  server_groups:
    - static_configs:
        - targets: [localhost:1]
`
)

// fastDiscovery shortens the wait for server groups to become ready, which
// every successful ApplyConfig does.
func fastDiscovery(t *testing.T) {
	old := servergroup.DiscoveryUpdateInterval
	servergroup.DiscoveryUpdateInterval = 10 * time.Millisecond
	t.Cleanup(func() { servergroup.DiscoveryUpdateInterval = old })
}

// TestRemoteWriteReAdded removes remote_write on reload and adds it back. The
// second pipeline registers the same collectors as the first, which panicked
// on duplicate registration until closing a pipeline unregistered them.
func TestRemoteWriteReAdded(t *testing.T) {
	fastDiscovery(t)
	ps, err := NewProxyStorage(noStepSubqueryInterval, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for i, raw := range []string{rwConfig, noRWConfig, rwConfig} {
		if err := ps.ApplyConfig(loadConfig(t, raw)); err != nil {
			t.Fatalf("reload %d: %v", i, err)
		}
	}
	defer ps.GetState().Cancel(nil)

	app := ps.Appender(context.Background())
	if _, err := app.Append(0, labels.FromStrings(labels.MetricName, "promxy_test_metric"), 1, 1); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := app.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// TestRemoteWriteSetupFailure fails a reload while building the remote_write
// pipeline (the WAL directory is a file). The reload must not leave the server
// groups or the half-built pipeline running, and must not leave collectors
// registered that would make the next attempt panic.
func TestRemoteWriteSetupFailure(t *testing.T) {
	fastDiscovery(t)
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())

	walDir := filepath.Join(t.TempDir(), "wal")
	if err := os.WriteFile(walDir, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	ps, err := NewProxyStorage(noStepSubqueryInterval, walDir)
	if err != nil {
		t.Fatal(err)
	}

	if err := ps.ApplyConfig(loadConfig(t, rwConfig)); err == nil {
		t.Fatal("ApplyConfig succeeded with a file as the WAL directory")
	}
	if got := len(ps.GetState().sgs); got != 0 {
		t.Fatalf("the failed config was applied: %d server groups in the current state", got)
	}

	// Once the WAL directory is usable, the same config applies.
	if err := os.Remove(walDir); err != nil {
		t.Fatal(err)
	}
	if err := ps.ApplyConfig(loadConfig(t, rwConfig)); err != nil {
		t.Fatalf("retry: %v", err)
	}
	ps.GetState().Cancel(nil)
}
