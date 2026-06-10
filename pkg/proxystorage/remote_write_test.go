package proxystorage

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
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
