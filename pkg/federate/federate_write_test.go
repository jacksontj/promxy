package federate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/sirupsen/logrus"
	"golang.org/x/net/http2"
)

// failingWriter is an http.ResponseWriter that accepts failAfter bytes and then
// fails every write with err. It records how many Write calls it saw, so a test
// can tell "stopped early" from "formatted the whole payload".
type failingWriter struct {
	failAfter int
	err       error

	hdr      http.Header
	accepted int
	writes   int
	status   int
}

func (f *failingWriter) Header() http.Header {
	if f.hdr == nil {
		f.hdr = http.Header{}
	}
	return f.hdr
}

func (f *failingWriter) WriteHeader(status int) { f.status = status }

func (f *failingWriter) Write(p []byte) (int, error) {
	f.writes++
	room := f.failAfter - f.accepted
	if room <= 0 {
		return 0, f.err
	}
	if room >= len(p) {
		f.accepted += len(p)
		return len(p), nil
	}
	f.accepted += room
	return room, f.err
}

// bigLoad builds a promqltest load block with n distinct series of one metric,
// large enough that the encoder's buffered writer flushes several times.
func bigLoad(n int) string {
	var b strings.Builder
	b.WriteString("load 1m\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "  fed_metric{instance=\"instance-%04d\",job=\"federation-test\"} 1 2 3\n", i)
	}
	return b.String()
}

// captureLogs redirects the standard logrus logger into a buffer of JSON lines
// for the duration of the test.
func captureLogs(t *testing.T, level logrus.Level) *bytes.Buffer {
	t.Helper()
	std := logrus.StandardLogger()
	out, formatter, lvl := std.Out, std.Formatter, std.GetLevel()
	var buf bytes.Buffer
	std.SetOutput(&buf)
	std.SetFormatter(&logrus.JSONFormatter{})
	std.SetLevel(level)
	t.Cleanup(func() {
		std.SetOutput(out)
		std.SetFormatter(formatter)
		std.SetLevel(lvl)
	})
	return &buf
}

func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) == 0 || lines[0] == "" {
		t.Fatal("no log output captured")
	}
	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &entry); err != nil {
		t.Fatalf("parsing log line %q: %v", lines[len(lines)-1], err)
	}
	return entry
}

// TestWriteSamplesStopsAtFirstError asserts the sample loop aborts as soon as a
// write fails, instead of formatting the rest of the payload into a dead
// socket.
func TestWriteSamplesStopsAtFirstError(t *testing.T) {
	const total = 2000
	vec := make([]floatSample, 0, total)
	for i := 0; i < total; i++ {
		vec = append(vec, floatSample{
			metric: labels.FromStrings(labels.MetricName, "fed_metric", "instance", fmt.Sprintf("instance-%04d", i)),
			t:      120000,
			f:      float64(i),
		})
	}

	boom := errors.New("boom")
	fw := &failingWriter{failAfter: 0, err: boom}

	written, err := writeSamples(fw, vec, []labels.Label{{Name: model.InstanceLabel, Value: ""}}, model.UnderscoreEscaping)
	if !errors.Is(err, boom) {
		t.Fatalf("expected the write error back, got %v", err)
	}
	if written == 0 || written >= total {
		t.Fatalf("expected to stop partway through the payload, wrote %d of %d", written, total)
	}
	// The whole payload is many buffer-flushes worth of data; stopping early
	// means the writer sees only the first failed flush (plus at most the
	// final Flush).
	if fw.writes > 2 {
		t.Fatalf("expected to stop after the first failed write, saw %d writes", fw.writes)
	}
}

// TestWriteSamplesWritesEverySample asserts the early-exit plumbing does not
// truncate a successful federation: every sample is written and the bytes match
// the vendored handler exactly for a multi-flush payload.
func TestWriteSamplesWritesEverySample(t *testing.T) {
	const series = 300
	st := promqltest.LoadedStorage(t, bigLoad(series))
	t.Cleanup(func() { st.Close() })

	now := timestamp.Time(120000)
	lookback := 5 * time.Minute
	target := "/federate?match%5B%5D=" + url.QueryEscape("fed_metric")

	h := New(st, lookback, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("unexpected fallback to vendored handler for text/plain request")
	}))
	h.now = func() time.Time { return now }
	h.SetExternalLabels(labels.EmptyLabels())

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))

	want, err := referenceFederate(st, lookback, now, labels.EmptyLabels(), httptest.NewRequest(http.MethodGet, target, nil))
	if err != nil {
		t.Fatalf("reference: %v", err)
	}
	got := rec.Body.Bytes()
	if !bytes.Equal(got, want) {
		t.Fatalf("output mismatch (%d vs %d bytes)", len(got), len(want))
	}
	if n := bytes.Count(got, []byte("fed_metric{")); n != series {
		t.Fatalf("expected %d sample lines, got %d", series, n)
	}
	if len(got) < 4096 {
		t.Fatalf("payload too small (%d bytes) to exercise a multi-flush write", len(got))
	}
}

// TestFederateWriteErrorLogging asserts a response body that could not be fully
// written is logged with how far it got, and that a client-side disconnect is
// not logged as an error.
func TestFederateWriteErrorLogging(t *testing.T) {
	st := promqltest.LoadedStorage(t, bigLoad(300))
	t.Cleanup(func() { st.Close() })

	for _, tc := range []struct {
		name    string
		err     error
		level   string
		message string
	}{
		{
			name:    "genuine_failure",
			err:     errors.New("kaboom"),
			level:   "error",
			message: "federation failed",
		},
		{
			name:    "client_disconnect",
			err:     fmt.Errorf("write tcp 10.0.0.1:9090->10.0.0.2:53124: %w", syscall.EPIPE),
			level:   "debug",
			message: "federation aborted: client went away",
		},
		{
			name:    "http2_stream_closed",
			err:     errors.New("http2: stream closed"),
			level:   "debug",
			message: "federation aborted: client went away",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureLogs(t, logrus.DebugLevel)

			h := New(st, 5*time.Minute, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Fatal("unexpected fallback to vendored handler for text/plain request")
			}))
			h.now = func() time.Time { return timestamp.Time(120000) }

			fw := &failingWriter{failAfter: 100, err: tc.err}
			target := "/federate?match%5B%5D=" + url.QueryEscape("fed_metric")
			h.ServeHTTP(fw, httptest.NewRequest(http.MethodGet, target, nil))

			entry := lastLogLine(t, logs)
			if entry["level"] != tc.level {
				t.Fatalf("level = %v, want %v (entry: %v)", entry["level"], tc.level, entry)
			}
			if entry["msg"] != tc.message {
				t.Fatalf("msg = %v, want %v", entry["msg"], tc.message)
			}
			written, _ := entry["samples_written"].(float64)
			total, _ := entry["samples_total"].(float64)
			if total != 300 {
				t.Fatalf("samples_total = %v, want 300", entry["samples_total"])
			}
			if written <= 0 || written >= total {
				t.Fatalf("samples_written = %v, want a partial count of %v", written, total)
			}
			if fw.writes > 2 {
				t.Fatalf("expected to stop after the first failed write, saw %d writes", fw.writes)
			}
		})
	}
}

// TestIsClientDisconnect covers the classification that decides between a debug
// line (the scrape's client went away) and an error line (a real failure).
func TestIsClientDisconnect(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"context_canceled", context.Canceled, true},
		{"wrapped_context_canceled", fmt.Errorf("writing response: %w", context.Canceled), true},
		{"epipe", syscall.EPIPE, true},
		{"wrapped_epipe", fmt.Errorf("write tcp: %w", syscall.EPIPE), true},
		{"econnreset", fmt.Errorf("write tcp: %w", syscall.ECONNRESET), true},
		{"net_err_closed", fmt.Errorf("write: %w", net.ErrClosed), true},
		{"http2_stream_error", http2.StreamError{StreamID: 3, Code: http2.ErrCodeCancel}, true},
		{"wrapped_http2_stream_error", fmt.Errorf("federate: %w", http2.StreamError{StreamID: 3, Code: http2.ErrCodeCancel}), true},
		{"http2_connection_error", http2.ConnectionError(http2.ErrCodeCancel), true},
		{"bundled_http2_stream_closed", errors.New("http2: stream closed"), true},
		{"bundled_client_disconnected", errors.New("client disconnected"), true},
		{"deadline_exceeded", context.DeadlineExceeded, false},
		{"short_write", io.ErrShortWrite, false},
		{"arbitrary", errors.New("something else went wrong"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := isClientDisconnect(tc.err); got != tc.want {
				t.Fatalf("isClientDisconnect(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
