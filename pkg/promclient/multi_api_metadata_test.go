package promclient

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// metadataCountAPI records how many Metadata calls it served, so tests can pin
// how many downstreams a fetch actually touches. Metadata is the only method
// implemented; the embedded (nil) API satisfies the rest of the interface.
type metadataCountAPI struct {
	API

	name  string // metric name this "target" reports metadata for
	err   error  // when set, every Metadata call fails with it
	calls atomic.Int64
}

func (a *metadataCountAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	a.calls.Add(1)
	if a.err != nil {
		return nil, a.err
	}
	return map[string][]v1.Metadata{
		a.name: {{Type: v1.MetricTypeHistogram, Help: "help"}},
	}, nil
}

// newMetadataTargets builds n targets sharing a single HA key. AddLabelClient
// is what exposes Key(), so it has to be the outermost wrapper for the targets
// to be grouped by anything other than the zero fingerprint.
func newMetadataTargets(n int, key model.LabelSet) ([]*metadataCountAPI, []API) {
	stubs := make([]*metadataCountAPI, n)
	apis := make([]API, n)
	for i := 0; i < n; i++ {
		stubs[i] = &metadataCountAPI{name: fmt.Sprintf("metric_%d", i)}
		apis[i] = &AddLabelClient{stubs[i], key}
	}
	return stubs, apis
}

func totalCalls(stubs ...[]*metadataCountAPI) int64 {
	var total int64
	for _, group := range stubs {
		for _, s := range group {
			total += s.calls.Load()
		}
	}
	return total
}

func TestMultiAPIMetadataOnePerKey(t *testing.T) {
	t.Run("one target per HA key", func(t *testing.T) {
		stubs, apis := newMetadataTargets(5, model.LabelSet{"a": "1"})
		m := NewMustMultiAPI(apis, model.Time(0), false, nil, 1, false)

		md, err := m.MetadataOnePerKey(context.Background(), "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := totalCalls(stubs); got != 1 {
			t.Fatalf("expected exactly 1 downstream metadata call, got %d", got)
		}
		if len(md) != 1 {
			t.Fatalf("expected metadata from a single target, got %v", md)
		}

		// For comparison: the fan-out variant hits every target.
		if _, err := m.Metadata(context.Background(), "", ""); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := totalCalls(stubs); got != 1+int64(len(stubs)) {
			t.Fatalf("expected Metadata to fan out to all %d targets, total calls %d", len(stubs), got)
		}
	})

	t.Run("rotates across replicas", func(t *testing.T) {
		stubs, apis := newMetadataTargets(3, model.LabelSet{"a": "1"})
		m := NewMustMultiAPI(apis, model.Time(0), false, nil, 1, false)

		for i := 0; i < len(stubs); i++ {
			if _, err := m.MetadataOnePerKey(context.Background(), "", ""); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		for i, s := range stubs {
			if got := s.calls.Load(); got != 1 {
				t.Fatalf("target %d served %d calls; expected each of the 3 replicas to serve exactly 1", i, got)
			}
		}
	})

	t.Run("fails over to another replica", func(t *testing.T) {
		stubs, apis := newMetadataTargets(3, model.LabelSet{"a": "1"})
		// The first call starts at index 0, so break that one.
		stubs[0].err = fmt.Errorf("target is down")
		m := NewMustMultiAPI(apis, model.Time(0), false, nil, 1, false)

		md, err := m.MetadataOnePerKey(context.Background(), "", "")
		if err != nil {
			t.Fatalf("expected failover to a healthy replica, got error: %v", err)
		}
		if _, ok := md["metric_1"]; !ok {
			t.Fatalf("expected metadata from the second replica, got %v", md)
		}
		if got := totalCalls(stubs); got != 2 {
			t.Fatalf("expected 1 failed + 1 successful call, got %d", got)
		}
	})

	t.Run("errors when the whole key is down", func(t *testing.T) {
		stubs, apis := newMetadataTargets(2, model.LabelSet{"a": "1"})
		for _, s := range stubs {
			s.err = fmt.Errorf("target is down")
		}
		m := NewMustMultiAPI(apis, model.Time(0), false, nil, 1, false)

		if _, err := m.MetadataOnePerKey(context.Background(), "", ""); err == nil {
			t.Fatal("expected an error when no replica of a key answers")
		}
		if got := totalCalls(stubs); got != 2 {
			t.Fatalf("expected every replica of the key to be tried, got %d calls", got)
		}
	})

	t.Run("queries every distinct key and merges", func(t *testing.T) {
		stubsA, apisA := newMetadataTargets(2, model.LabelSet{"a": "1"})
		stubsB, apisB := newMetadataTargets(2, model.LabelSet{"a": "2"})
		// Distinguish the second key's metric names from the first key's.
		for i, s := range stubsB {
			s.name = fmt.Sprintf("other_metric_%d", i)
		}
		m := NewMustMultiAPI(append(apisA, apisB...), model.Time(0), false, nil, 1, false)

		md, err := m.MetadataOnePerKey(context.Background(), "", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got := totalCalls(stubsA, stubsB); got != 2 {
			t.Fatalf("expected 1 call per key (2 total), got %d", got)
		}
		if len(md) != 2 {
			t.Fatalf("expected the two keys' metadata merged, got %v", md)
		}
		if _, ok := md["metric_0"]; !ok {
			t.Fatalf("missing first key's metadata: %v", md)
		}
		if _, ok := md["other_metric_0"]; !ok {
			t.Fatalf("missing second key's metadata: %v", md)
		}
	})
}
