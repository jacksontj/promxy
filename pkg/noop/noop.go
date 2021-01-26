package noop

import (
	"context"

	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/storage"
)

type noopStorage struct{}

// NewNoopStorage returns a new noop storage
func NewNoopStorage() storage.Storage {
	return &noopStorage{}
}

// Querier returns a new Querier on the storage.
func (n *noopStorage) Querier(ctx context.Context, mint, maxt int64) (storage.Querier, error) {
	return storage.NoopQuerier(), nil
}

// StartTime returns the oldest timestamp stored in the storage.
func (n *noopStorage) StartTime() (int64, error) {
	return 0, nil
}

// Appender returns a new appender against the storage.
func (n *noopStorage) Appender(_ context.Context) storage.Appender {
	return NewNoopAppender()
}

// Close closes the storage and all its underlying resources.
func (n *noopStorage) Close() error {
	return nil
}

// TODO: never work? We aren't a tsdb storage
func (n *noopStorage) ChunkQuerier(ctx context.Context, mint, maxt int64) (storage.ChunkQuerier, error) {
	return nil, nil
}

type noopAppender struct{}

// NewNoopAppender returns a new noop storage Appender
func NewNoopAppender() storage.Appender {
	return &noopAppender{}
}

func (a *noopAppender) Add(l labels.Labels, t int64, v float64) (uint64, error) {
	return 0, nil
}

func (a *noopAppender) AddFast(ref uint64, t int64, v float64) error {
	return nil
}

// Commit submits the collected samples and purges the batch.
func (a *noopAppender) Commit() error   { return nil }
func (a *noopAppender) Rollback() error { return nil }
