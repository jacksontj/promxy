package proxystorage

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promapi"
)

var errDownstream = errors.New("downstream exploded")

// errStub fails every request the way a real client does: not by returning an
// error alongside the SeriesSet, but by returning a SeriesSet whose Err() is
// set. That distinction is the whole point of this test -- the pushdown sites
// used to check a `var err error` that nothing ever assigned to.
type errStub struct{ stubAPI }

func (a *errStub) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, errDownstream)
}

func (a *errStub) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	return promapi.NewSeriesSet(nil, nil, errDownstream)
}

// TestPushdownSurfacesDownstreamError checks every pushdown site reports a
// failed downstream rather than treating it as an empty result.
//
// This passed before the dead `err` checks were removed, because an errored
// SeriesSet stashed into UnexpandedSeriesSet still surfaces when the engine
// expands it. It is here so that remains true now that those sites check
// result.Err() at the point of the request instead.
func TestPushdownSurfacesDownstreamError(t *testing.T) {
	atStr := strconv.FormatInt(e2eGridT0, 10)

	exprs := []string{
		// Range pushdowns, one per site.
		"sum(foo)",
		"count(foo)",
		`count_values("l", foo)`,
		"topk(1, foo)",
		"foo",
		"foo > 1",
		"min(foo) > 1",
		"sort(foo)",
		// And again with @, which routes through the instant-query path.
		"sum(foo @ " + atStr + ")",
		"foo @ " + atStr,
		"min(foo @ " + atStr + ") > 1",
		"sort(foo @ " + atStr + ")",
	}

	startSec, endSec := e2eGridT0, e2eGridT0+(e2eN-1)*e2eStep

	for _, expr := range exprs {
		t.Run(expr, func(t *testing.T) {
			ps, eng := newProxyStorage(t, &errStub{})

			q, err := eng.NewRangeQuery(context.Background(), ps, nil, expr,
				time.Unix(startSec, 0), time.Unix(endSec, 0), time.Duration(e2eStep)*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			res := q.Exec(context.Background())

			if res.Err == nil {
				t.Fatalf("query succeeded against a failing downstream; got %v", res.Value)
			}
			if !strings.Contains(res.Err.Error(), errDownstream.Error()) {
				t.Errorf("error %q does not mention the downstream failure %q", res.Err, errDownstream)
			}
		})
	}
}
