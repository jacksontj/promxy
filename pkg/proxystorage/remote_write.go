package proxystorage

import (
	"fmt"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb/agent"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/logging"
)

// remoteWrite is the remote_write pipeline: an agent-mode WAL-only DB that the
// rule manager appends to, and the remote.Storage whose queue managers tail
// that WAL and ship it.
type remoteWrite struct {
	storage *remote.Storage
	db      *agent.DB
	close   func() error
}

// newRemoteWrite builds the remote_write pipeline for cfg, with its WAL under
// walDir, or under a temporary directory removed on close if walDir is empty.
// If it fails, it releases everything it built.
func newRemoteWrite(cfg *config.Config, walDir string) (_ *remoteWrite, err error) {
	ephemeral := walDir == ""
	if ephemeral {
		dir, err := os.MkdirTemp("", "promxy-remote-wal-")
		if err != nil {
			return nil, fmt.Errorf("creating remote_write WAL dir: %w", err)
		}
		walDir = dir
	}

	// remote.Storage registers collectors it never unregisters, so a second
	// pipeline (a retry after a failure, or remote_write re-added on reload)
	// would panic on duplicate registration. Unregister them on close.
	reg := &unregisteringRegisterer{Registerer: prometheus.DefaultRegisterer}

	var cleanup []func() error
	release := func() error {
		var first error
		for i := len(cleanup) - 1; i >= 0; i-- {
			if err := cleanup[i](); err != nil && first == nil {
				first = err
			}
		}
		reg.unregisterAll()
		if ephemeral {
			os.RemoveAll(walDir)
		}
		return first
	}
	defer func() {
		if err != nil {
			release()
		}
	}()

	rwLogger := logging.NewLogger(logrus.WithField("component", "remote_write").Logger)
	rs := remote.NewStorage(
		rwLogger,
		reg,
		func() (int64, error) { return 0, nil },
		walDir,
		1*time.Second,
		noopScrapeManager{},
	)
	cleanup = append(cleanup, rs.Close)

	// promxy has no local TSDB writing a WAL, so remote.Storage's queue
	// managers have nothing to tail. Run an agent-mode WAL-only DB whose
	// appender writes the WAL that those queue managers consume. Without
	// this the WAL watcher fails with "error tailing WAL ... no such file
	// or directory" and no samples are ever shipped (see issue #771).
	db, err := agent.Open(rwLogger, reg, rs, walDir, agent.DefaultOptions())
	if err != nil {
		return nil, fmt.Errorf("creating remote_write WAL: %w", err)
	}
	cleanup = append(cleanup, db.Close)

	// Wake the queue managers' WAL watchers as soon as samples are committed.
	db.SetWriteNotified(rs)
	if err := rs.ApplyConfig(cfg); err != nil {
		return nil, err
	}

	return &remoteWrite{storage: rs, db: db, close: release}, nil
}

// unregisteringRegisterer records the collectors registered through it, so
// they can all be unregistered once their owner is closed.
type unregisteringRegisterer struct {
	prometheus.Registerer

	mtx        sync.Mutex
	collectors []prometheus.Collector
}

func (u *unregisteringRegisterer) Register(c prometheus.Collector) error {
	if err := u.Registerer.Register(c); err != nil {
		return err
	}
	u.mtx.Lock()
	u.collectors = append(u.collectors, c)
	u.mtx.Unlock()
	return nil
}

func (u *unregisteringRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := u.Register(c); err != nil {
			panic(err)
		}
	}
}

func (u *unregisteringRegisterer) Unregister(c prometheus.Collector) bool {
	u.mtx.Lock()
	for i, rc := range u.collectors {
		if sameCollector(rc, c) {
			u.collectors = append(u.collectors[:i], u.collectors[i+1:]...)
			break
		}
	}
	u.mtx.Unlock()
	return u.Registerer.Unregister(c)
}

// sameCollector reports whether a and b are the same collector, without
// panicking on collector types that aren't comparable.
func sameCollector(a, b prometheus.Collector) bool {
	ta := reflect.TypeOf(a)
	return ta == reflect.TypeOf(b) && ta.Comparable() && a == b
}

func (u *unregisteringRegisterer) unregisterAll() {
	u.mtx.Lock()
	cs := u.collectors
	u.collectors = nil
	u.mtx.Unlock()
	for _, c := range cs {
		u.Registerer.Unregister(c)
	}
}
