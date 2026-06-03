package test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage/remote"
	yaml "gopkg.in/yaml.v2"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/proxystorage"
)

// rwReceiver is a minimal remote_write endpoint that decodes incoming
// (snappy-compressed protobuf) write requests and records the samples it sees,
// keyed by series. It is used to assert that promxy actually ships samples
// end-to-end over the remote_write path (regression for issue #771).
type rwReceiver struct {
	mu      sync.Mutex
	samples map[string][]float64
	posts   int
}

func newRWReceiver() *rwReceiver {
	return &rwReceiver{samples: map[string][]float64{}}
}

func (r *rwReceiver) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	wr, err := remote.DecodeWriteRequest(req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	r.mu.Lock()
	r.posts++
	for _, ts := range wr.Timeseries {
		b := labels.NewScratchBuilder(len(ts.Labels))
		for _, l := range ts.Labels {
			b.Add(l.Name, l.Value)
		}
		b.Sort()
		key := b.Labels().String()
		for _, s := range ts.Samples {
			r.samples[key] = append(r.samples[key], s.Value)
		}
	}
	r.mu.Unlock()

	w.WriteHeader(http.StatusNoContent)
}

// valuesFor returns the values received for the series matching lbls (exact
// label-set match).
func (r *rwReceiver) valuesFor(lbls labels.Labels) []float64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]float64, len(r.samples[lbls.String()]))
	copy(out, r.samples[lbls.String()])
	return out
}

// rawRemoteWriteConfig points promxy's remote_write at the receiver and tunes
// the queue so samples flush quickly (the default batch_send_deadline is 5s).
// It intentionally has no server_groups -- this exercises the remote_write
// (appender/WAL) path in isolation.
const rawRemoteWriteConfig = `
remote_write:
  - url: %s
    queue_config:
      batch_send_deadline: 200ms
      min_shards: 1
      max_shards: 1
      capacity: 1000
`

// TestRemoteWriteEndToEnd is an end-to-end regression test for issue #771: with
// remote_write configured, samples appended through promxy's storage (as the
// rule manager does) must actually be shipped to the remote endpoint. Before the
// fix promxy never wrote a WAL for the queue managers to tail, so the watcher
// errored every tick and zero samples were ever delivered.
func TestRemoteWriteEndToEnd(t *testing.T) {
	recv := newRWReceiver()
	srv := httptest.NewServer(recv)
	defer srv.Close()

	cfg := &proxyconfig.Config{}
	if err := yaml.Unmarshal([]byte(fmt.Sprintf(rawRemoteWriteConfig, srv.URL)), cfg); err != nil {
		t.Fatalf("unmarshal config: %v", err)
	}

	ps, err := proxystorage.NewProxyStorage(func(int64) int64 { return 0 }, t.TempDir())
	if err != nil {
		t.Fatalf("NewProxyStorage: %v", err)
	}
	if err := ps.ApplyConfig(cfg); err != nil {
		t.Fatalf("ApplyConfig: %v", err)
	}
	defer ps.GetState().Cancel(nil)

	series := labels.FromStrings(labels.MetricName, "promxy_e2e_metric", "src", "issue771")

	// The WAL watcher only ships samples with a timestamp newer than when it
	// started, and a single write notification can race and be dropped (falling
	// back to a 15s read timer). Mirror the rule manager by appending on an
	// interval with current-time timestamps until the receiver observes the
	// series (or we time out).
	const wantValue = 42.0
	stopAppending := make(chan struct{})
	var appendErr error
	var appendOnce sync.Once
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopAppending:
				return
			case <-ticker.C:
				app := ps.Appender(context.Background())
				if _, err := app.Append(0, series, timestamp.FromTime(time.Now()), wantValue); err != nil {
					appendOnce.Do(func() { appendErr = fmt.Errorf("append: %w", err) })
					return
				}
				if err := app.Commit(); err != nil {
					appendOnce.Do(func() { appendErr = fmt.Errorf("commit: %w", err) })
					return
				}
			}
		}
	}()

	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-deadline:
			close(stopAppending)
			t.Fatalf("timed out waiting for remote_write samples; receiver got %d POST(s), no matching series", recv.posts)
		case <-tick.C:
			if appendErr != nil {
				close(stopAppending)
				t.Fatalf("error while appending: %v", appendErr)
			}
			vals := recv.valuesFor(series)
			if len(vals) > 0 {
				for _, v := range vals {
					if v != wantValue {
						close(stopAppending)
						t.Fatalf("received unexpected sample value: got %v want %v", v, wantValue)
					}
				}
				close(stopAppending)
				return // success: samples made it end-to-end
			}
		}
	}
}
