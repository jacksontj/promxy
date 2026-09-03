package servergroup

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync/atomic"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/promclient"
)

// metadataTarget stands in for one downstream prometheus. It serves a fixed
// metadata dump and records how many calls (and how many bytes of response) it
// served, so tests can pin the fan-out of a metadata refresh.
type metadataTarget struct {
	md        map[string][]v1.Metadata
	mdSize    int64 // marshaled size of md, i.e. bytes on the wire per call
	err       error
	calls     atomic.Int64
	bytesSent atomic.Int64
}

func (t *metadataTarget) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	t.calls.Add(1)
	if t.err != nil {
		return nil, t.err
	}
	t.bytesSent.Add(t.mdSize)
	return t.md, nil
}

func (t *metadataTarget) LabelNames(context.Context, []string, time.Time, time.Time) ([]string, v1.Warnings, error) {
	return nil, nil, nil
}

func (t *metadataTarget) LabelValues(context.Context, string, []string, time.Time, time.Time) (model.LabelValues, v1.Warnings, error) {
	return nil, nil, nil
}

func (t *metadataTarget) Query(context.Context, string, time.Time) storage.SeriesSet {
	return storage.EmptySeriesSet()
}

func (t *metadataTarget) QueryRange(context.Context, string, v1.Range) storage.SeriesSet {
	return storage.EmptySeriesSet()
}

func (t *metadataTarget) Series(context.Context, []string, time.Time, time.Time) ([]model.LabelSet, v1.Warnings, error) {
	return nil, nil, nil
}

func (t *metadataTarget) GetValue(context.Context, time.Time, time.Time, []*labels.Matcher) storage.SeriesSet {
	return storage.EmptySeriesSet()
}

func (t *metadataTarget) QueryExemplars(context.Context, string, time.Time, time.Time) ([]v1.ExemplarQueryResult, error) {
	return nil, nil
}

// buildMetadata produces a metadata dump of the size a real deployment returns:
// histograms plus a much larger tail of other types, each with a help string.
func buildMetadata(names int) map[string][]v1.Metadata {
	md := make(map[string][]v1.Metadata, names)
	for i := 0; i < names; i++ {
		t := v1.MetricTypeCounter
		if i%50 == 0 {
			t = v1.MetricTypeHistogram
		}
		name := fmt.Sprintf("some_service_subsystem_metric_name_%d", i)
		md[name] = []v1.Metadata{{
			Type: t,
			Help: "Help text for " + name + ", which is typically a full sentence describing the metric.",
			Unit: "",
		}}
	}
	return md
}

// newServerGroupMultiAPI wraps targets the way loadTargetGroupMap does: label
// addition then a per-target error wrap, all behind a MultiAPI.
func newServerGroupMultiAPI(t *testing.T, targets []*metadataTarget) *promclient.MultiAPI {
	t.Helper()
	apis := make([]promclient.API, len(targets))
	for i, target := range targets {
		var api promclient.API = &promclient.AddLabelClient{API: target, Labels: model.LabelSet{"sg": "0"}}
		apis[i] = &promclient.ErrorWrap{A: api, Msg: fmt.Sprintf("error in target=%d", i)}
	}
	m, err := promclient.NewMultiAPI(apis, model.Time(0), false, nil, 1, false)
	if err != nil {
		t.Fatalf("error building multi api: %v", err)
	}
	return m
}

func newMetadataTargetSet(t *testing.T, count, names int) []*metadataTarget {
	t.Helper()
	md := buildMetadata(names)
	b, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("error marshaling metadata: %v", err)
	}
	targets := make([]*metadataTarget, count)
	for i := range targets {
		// Each replica gets its own copy: MultiAPI's fan-out merge writes into
		// the first response it gets, so a shared map would be a data race.
		own := make(map[string][]v1.Metadata, len(md))
		for k, v := range md {
			own[k] = v
		}
		targets[i] = &metadataTarget{md: own, mdSize: int64(len(b))}
	}
	return targets
}

func testLogger() *logrus.Entry {
	l := logrus.New()
	l.SetOutput(io.Discard)
	return logrus.NewEntry(l)
}

func totalTargetCalls(targets []*metadataTarget) int64 {
	var n int64
	for _, target := range targets {
		n += target.calls.Load()
	}
	return n
}

func totalTargetBytes(targets []*metadataTarget) int64 {
	var n int64
	for _, target := range targets {
		n += target.bytesSent.Load()
	}
	return n
}

func TestHistogramMetadataCacheRefreshQueriesOneTarget(t *testing.T) {
	const (
		targetCount = 20
		nameCount   = 5000
	)

	targets := newMetadataTargetSet(t, targetCount, nameCount)
	api := newServerGroupMultiAPI(t, targets)

	var c histogramMetadataCache
	c.refresh(context.Background(), api, testLogger())

	if got := totalTargetCalls(targets); got != 1 {
		t.Fatalf("expected the refresh to query exactly 1 of %d targets, got %d calls", targetCount, got)
	}
	if !c.Contains("some_service_subsystem_metric_name_0") {
		t.Fatal("expected the refresh to populate the histogram name set")
	}
	if c.Contains("some_service_subsystem_metric_name_1") {
		t.Fatal("non-histogram metric leaked into the histogram name set")
	}

	refreshBytes := totalTargetBytes(targets)

	// Fan-out cost for the same snapshot, for comparison.
	fanoutTargets := newMetadataTargetSet(t, targetCount, nameCount)
	fanoutAPI := newServerGroupMultiAPI(t, fanoutTargets)
	if _, err := fanoutAPI.Metadata(context.Background(), "", ""); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	fanoutBytes := totalTargetBytes(fanoutTargets)

	t.Logf("metadata bytes per refresh: %d (one target) vs %d (all %d targets)", refreshBytes, fanoutBytes, targetCount)
	if refreshBytes*int64(targetCount) != fanoutBytes {
		t.Fatalf("expected the refresh to transfer 1/%d of the fan-out bytes: %d vs %d", targetCount, refreshBytes, fanoutBytes)
	}
}

func TestHistogramMetadataCacheRefreshFailsOver(t *testing.T) {
	t.Run("falls back to a healthy target", func(t *testing.T) {
		targets := newMetadataTargetSet(t, 3, 100)
		targets[0].err = fmt.Errorf("connection refused")
		api := newServerGroupMultiAPI(t, targets)

		var c histogramMetadataCache
		c.refresh(context.Background(), api, testLogger())

		if !c.Contains("some_service_subsystem_metric_name_0") {
			t.Fatal("expected the refresh to fail over to a healthy target and populate the cache")
		}
		if got := totalTargetCalls(targets); got != 2 {
			t.Fatalf("expected 1 failed + 1 successful call, got %d", got)
		}
	})

	t.Run("keeps the previous snapshot when every target is down", func(t *testing.T) {
		targets := newMetadataTargetSet(t, 3, 100)
		api := newServerGroupMultiAPI(t, targets)

		var c histogramMetadataCache
		c.refresh(context.Background(), api, testLogger())
		if !c.Contains("some_service_subsystem_metric_name_0") {
			t.Fatal("expected the first refresh to populate the cache")
		}

		for _, target := range targets {
			target.err = fmt.Errorf("connection refused")
		}
		c.refresh(context.Background(), api, testLogger())

		if !c.Contains("some_service_subsystem_metric_name_0") {
			t.Fatal("expected a failed refresh to keep the previous snapshot")
		}
		if got := totalTargetCalls(targets); got != 1+3 {
			t.Fatalf("expected every target to be tried before giving up, got %d calls", got)
		}
	})
}
