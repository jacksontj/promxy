// Copyright 2026 The Prometheus Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package promqltest

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/prometheus/prometheus/util/testutil"
)

// stableStorage is a storage.Storage with a stable identity that delegates
// to a swappable inner. Consumers (notably an HTTP API server backed by
// test.Storage()) keep their reference to the stableStorage across `clear`
// commands inside the parsed test; clear() only resets stableStorage.inner.
type stableStorage struct {
	mu    sync.RWMutex
	inner storage.Storage
}

func (s *stableStorage) get() storage.Storage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inner
}

func (s *stableStorage) set(inner storage.Storage) {
	s.mu.Lock()
	s.inner = inner
	s.mu.Unlock()
}

func (s *stableStorage) Querier(mint, maxt int64) (storage.Querier, error) {
	if inner := s.get(); inner != nil {
		return inner.Querier(mint, maxt)
	}
	return errStorageQuerier{}, nil
}

func (s *stableStorage) ChunkQuerier(mint, maxt int64) (storage.ChunkQuerier, error) {
	if inner := s.get(); inner != nil {
		return inner.ChunkQuerier(mint, maxt)
	}
	return errStorageChunkQuerier{}, nil
}

func (s *stableStorage) StartTime() (int64, error) {
	if inner := s.get(); inner != nil {
		return inner.StartTime()
	}
	return 0, nil
}

func (s *stableStorage) Appender(ctx context.Context) storage.Appender {
	return s.get().Appender(ctx)
}

func (s *stableStorage) Close() error {
	if inner := s.get(); inner != nil {
		return inner.Close()
	}
	return nil
}

type errStorageQuerier struct{}

func (errStorageQuerier) Select(_ context.Context, _ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.SeriesSet {
	return storage.EmptySeriesSet()
}
func (errStorageQuerier) LabelValues(_ context.Context, _ string, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (errStorageQuerier) LabelNames(_ context.Context, _ *storage.LabelHints, _ ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	return nil, nil, nil
}
func (errStorageQuerier) Close() error { return nil }

type errStorageChunkQuerier struct{ errStorageQuerier }

func (errStorageChunkQuerier) Select(_ context.Context, _ bool, _ *storage.SelectHints, _ ...*labels.Matcher) storage.ChunkSeriesSet {
	return storage.EmptyChunkSeriesSet()
}

// Test is the exported handle for parsed PromQL tests. It carries a parsed
// command list, a backing storage, and a query engine. This pre-3.x shape
// is preserved for fork consumers (notably promxy) that need to attach a
// NodeReplacer to the engine before running, or to swap the storage for
// a wrapping layer between parsing and execution.
//
// Storage indirection: Test owns a stableStorage wrapper. Each `clear`
// command in the parsed input replaces the wrapper's inner with a fresh
// teststorage; consumers holding the wrapper (e.g. an HTTP API server)
// continue to see live data. SetStorage is independent: it installs a
// caller-supplied storage as the runtime storage seen by the inner test
// runner; the caller's storage is preserved across clear commands so that
// the engine continues to query through the wrapping layer.
type Test struct {
	t        *test
	engine   *promql.Engine
	external *stableStorage // stable storage handed out via Storage()
	override storage.Storage // installed via SetStorage; nil means "use external directly"
	closed   bool
}

// NewTest parses the given test input and returns a Test ready to Run.
func NewTest(t testutil.T, input string) (*Test, error) {
	wrapper := Test{external: &stableStorage{}}
	open := func(_ testutil.T) storage.Storage {
		// Reset the external wrapper's inner with a fresh teststorage on each
		// clear (and once on creation, since newTest invokes the open function
		// from inside its initial clear).
		wrapper.external.set(newTestStorage(t))
		// If the caller installed an override storage via SetStorage, hand
		// the inner test runner that override; otherwise hand it the stable
		// wrapper directly.
		if wrapper.override != nil {
			return wrapper.override
		}
		return wrapper.external
	}
	inner, err := newTest(t, input, false, open)
	if err != nil {
		return nil, err
	}
	wrapper.t = inner
	wrapper.engine = promql.NewEngine(promql.EngineOpts{
		Logger:                   nil,
		Reg:                      nil,
		MaxSamples:               10000,
		Timeout:                  100 * time.Second,
		NoStepSubqueryIntervalFn: func(int64) int64 { return durationMilliseconds(1 * time.Minute) },
		EnableAtModifier:         true,
		EnableNegativeOffset:     true,
		EnableDelayedNameRemoval: true,
	})
	return &wrapper, nil
}

// Storage returns a stable storage.Storage that survives clear commands
// inside the parsed test. Consumers that want a fixed read/write handle
// across the test lifetime should use this.
func (t *Test) Storage() storage.Storage { return t.external }

// Queryable returns Storage as a storage.Queryable.
func (t *Test) Queryable() storage.Queryable { return t.external }

// QueryEngine returns the query engine the Test will use when Run is called.
func (t *Test) QueryEngine() *promql.Engine { return t.engine }

// SetStorage installs a caller-supplied storage as the runtime storage the
// inner test runner will use for load/eval. The override is preserved
// across `clear` commands; clear still resets the external wrapper's
// inner (so any code that holds the wrapper, like an HTTP API server,
// continues to see freshly-loaded data).
func (t *Test) SetStorage(s storage.Storage) {
	t.override = s
	t.t.SetStorage(s)
}

// Run executes all parsed commands against the Test's engine and storage.
func (t *Test) Run() error {
	for _, cmd := range t.t.cmds {
		if err := t.t.exec(cmd, t.engine); err != nil {
			return err
		}
	}
	return nil
}

// Close releases all resources held by the Test. Safe to call multiple times.
func (t *Test) Close() {
	if t.t == nil || t.closed {
		return
	}
	t.closed = true
	if t.t.storage != nil {
		// The storage may have been wrapped via SetStorage and shares its
		// underlying handle with code paths that already closed it; recover
		// to keep Close idempotent for those cases.
		func() {
			defer func() { _ = recover() }()
			_ = t.t.storage.Close()
		}()
	}
	if t.t.cancelCtx != nil {
		t.t.cancelCtx()
	}
}
