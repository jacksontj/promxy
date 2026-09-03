package proxystorage

import (
	"context"
	"sync"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promclient"
)

// concurrencyStub records the peak number of downstream calls in flight at
// once. Each call blocks for stubLatency to stand in for a round trip.
type concurrencyStub struct {
	stubAPI

	mu       sync.Mutex
	inFlight int
	peak     int
	calls    int
}

const stubLatency = 100 * time.Millisecond

func (a *concurrencyStub) roundTrip() {
	a.mu.Lock()
	a.inFlight++
	a.calls++
	if a.inFlight > a.peak {
		a.peak = a.inFlight
	}
	a.mu.Unlock()

	time.Sleep(stubLatency)

	a.mu.Lock()
	a.inFlight--
	a.mu.Unlock()
}

func (a *concurrencyStub) stats() (calls, peak int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.calls, a.peak
}

func (a *concurrencyStub) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	a.roundTrip()
	return promclient.ModelValueToSeriesSet(model.Vector{}, nil, nil)
}

func (a *concurrencyStub) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	a.roundTrip()
	return promclient.ModelValueToSeriesSet(model.Matrix{}, nil, nil)
}

func (a *concurrencyStub) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	a.roundTrip()
	return promclient.ModelValueToSeriesSet(model.Matrix{}, nil, nil)
}

// TestParallelDownstreamFetch pins the reason parser.Walk fans out when a
// NodeReplacer is installed: sibling subtrees of a query each cost a
// downstream round trip, and they have to overlap. Serialising them makes a
// fan-out query as slow as the sum of its parts (promxy#809 fixed the
// crash by making read-only walks sequential -- this guards the other half,
// that the fetching walk stayed parallel).
func TestParallelDownstreamFetch(t *testing.T) {
	for _, tc := range []struct {
		expr      string
		wantCalls int
	}{
		{`sum(a) + sum(b)`, 2},
		{`sum(a) + sum(b) + sum(c) + sum(d)`, 4},
		{`a + b`, 2},
		{`rate(a[5m]) + rate(b[5m])`, 2},
	} {
		t.Run(tc.expr, func(t *testing.T) {
			stub := &concurrencyStub{}
			ps, eng := newProxyStorage(t, stub)

			runRange(t, ps, eng, tc.expr, e2eGridT0, e2eGridT0+e2eStep*e2eN)

			calls, peak := stub.stats()
			if calls != tc.wantCalls {
				t.Fatalf("downstream calls: got %d, want %d", calls, tc.wantCalls)
			}
			if peak != tc.wantCalls {
				t.Fatalf("peak concurrent downstream calls: got %d, want %d -- sibling fetches were serialised", peak, tc.wantCalls)
			}
		})
	}
}
