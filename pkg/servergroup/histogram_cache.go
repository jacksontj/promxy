package servergroup

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/sirupsen/logrus"

	"github.com/jacksontj/promxy/pkg/promclient"
)

// histogramMetadataCache holds the set of metric names whose upstream type
// is HISTOGRAM, refreshed periodically from /api/v1/metadata. It exists so
// that promxy can route any query referencing a histogram metric via
// remote_read even when the query doesn't use one of the histogram-only
// PromQL functions (which the AST walker can detect on its own).
//
// The cache is name-keyed, not series-keyed: a deployment with millions of
// active series typically has only thousands of distinct metric names, and
// the histogram subset is usually a small fraction of that. Memory grows
// with histogram-metric-name count, not cardinality.
//
// A nil *histogramMetadataCache is valid: Contains returns false.
type histogramMetadataCache struct {
	// names points at a frozen map[string]struct{}; replaced atomically on
	// each refresh so readers never see a half-updated set.
	names atomic.Pointer[map[string]struct{}]

	startOnce sync.Once
}

// Contains reports whether name is known to be a histogram metric in this
// server group's most recent metadata snapshot. Returns false when the
// cache is disabled, when the first refresh hasn't completed, or when the
// name simply isn't in the histogram set. Callers should treat false as
// "not known to be a histogram" (conservative — don't change behaviour).
func (c *histogramMetadataCache) Contains(name string) bool {
	if c == nil {
		return false
	}
	m := c.names.Load()
	if m == nil {
		return false
	}
	_, ok := (*m)[name]
	return ok
}

// start launches the background refresh loop the first time it's called.
// Subsequent calls are no-ops. getAPI is called on each tick to fetch the
// current API client (which can change as service discovery updates the
// server group's target set).
func (c *histogramMetadataCache) start(ctx context.Context, getAPI func() promclient.API, refresh time.Duration, logger *logrus.Entry) {
	if c == nil || refresh <= 0 {
		return
	}
	c.startOnce.Do(func() {
		go c.run(ctx, getAPI, refresh, logger)
	})
}

func (c *histogramMetadataCache) run(ctx context.Context, getAPI func() promclient.API, refresh time.Duration, logger *logrus.Entry) {
	// One immediate refresh so the first query after startup can already
	// benefit; the ticker takes over after that.
	c.refresh(ctx, getAPI(), logger)
	t := time.NewTicker(refresh)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.refresh(ctx, getAPI(), logger)
		}
	}
}

func (c *histogramMetadataCache) refresh(ctx context.Context, api promclient.API, logger *logrus.Entry) {
	if api == nil {
		// Service discovery hasn't populated targets yet. Skip silently;
		// the next tick will retry. Keep the previous snapshot in place.
		return
	}
	fctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	md, err := api.Metadata(fctx, "", "")
	if err != nil {
		logger.WithError(err).Warn("histogram metadata cache refresh failed; keeping previous snapshot")
		return
	}
	// Extract just the histogram names — that's all the routing decision needs.
	names := make(map[string]struct{})
	for name, entries := range md {
		for _, e := range entries {
			if e.Type == v1.MetricTypeHistogram {
				names[name] = struct{}{}
				break
			}
		}
	}
	c.names.Store(&names)
	logger.Debugf("histogram metadata cache refreshed (%d histogram metrics tracked)", len(names))
}
