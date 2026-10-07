package test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDownstreamErrors runs promxy's full HTTP API in front of a backend that
// fails, and checks what the client of promxy sees: the HTTP status and the
// error text. The backend failure has to survive the whole chain (decode,
// target ErrorWrap, server group MultiAPI, servergroup ErrorWrap, the
// ProxyStorage MultiAPI, the engine and the API's error mapping).
func TestDownstreamErrors(t *testing.T) {
	type backend struct {
		status      int
		contentType string
		body        string
	}
	cases := []struct {
		name    string
		backend backend
		// wantStatus/wantType are promxy's answer.
		wantStatus int
		wantType   string
		// wantMsg must appear in promxy's error message.
		wantMsg string
	}{
		{
			name:       "json_timeout_envelope",
			backend:    backend{http.StatusServiceUnavailable, "application/json", `{"status":"error","errorType":"timeout","error":"query timed out in expression evaluation"}`},
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "timeout",
			wantMsg:    "query timed out in expression evaluation",
		},
		{
			name:       "json_canceled_envelope",
			backend:    backend{http.StatusServiceUnavailable, "application/json", `{"status":"error","errorType":"canceled","error":"query was canceled in expression evaluation"}`},
			wantStatus: 499, // the vendored API's statusClientClosedConnection
			wantType:   "canceled",
			wantMsg:    "query was canceled in expression evaluation",
		},
		{
			name:       "json_execution_envelope",
			backend:    backend{http.StatusUnprocessableEntity, "application/json", `{"status":"error","errorType":"execution","error":"many-to-many matching not allowed"}`},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "many-to-many matching not allowed",
		},
		{
			name:       "plain_text_503",
			backend:    backend{http.StatusServiceUnavailable, "text/plain", "too many outstanding requests\n"},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "too many outstanding requests",
		},
		{
			name:       "html_502",
			backend:    backend{http.StatusBadGateway, "text/html", "<html><body>502 Bad Gateway</body></html>"},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "502 Bad Gateway",
		},
	}

	endpoints := []struct {
		path string
		// alwaysExec is set for the endpoints whose vendored Prometheus
		// handler answers every storage error as 422 execution, without
		// looking at the error's class.
		alwaysExec bool
	}{
		{"/api/v1/query?query=up", false},
		{"/api/v1/query_range?query=up&start=0&end=60&step=15", false},
		{"/api/v1/series?match[]=up", false},
		{"/api/v1/labels", true},
		{"/api/v1/label/job/values", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			backendSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", tc.backend.contentType)
				w.WriteHeader(tc.backend.status)
				io.WriteString(w, tc.backend.body)
			}))
			defer backendSrv.Close()

			ps := getProxyStorage(fmt.Sprintf(rawPSConfig, strings.TrimPrefix(backendSrv.URL, "http://")))
			defer ps.GetState().Cancel(nil)
			srv, addr, stop := startAPIForTest(ps)
			defer func() { srv.Shutdown(context.Background()); <-stop }()

			for _, ep := range endpoints {
				t.Run(strings.SplitN(ep.path, "?", 2)[0], func(t *testing.T) {
					wantStatus, wantType := tc.wantStatus, tc.wantType
					if ep.alwaysExec {
						wantStatus, wantType = http.StatusUnprocessableEntity, "execution"
					}
					resp, err := http.Get("http://" + addr + ep.path)
					if err != nil {
						t.Fatal(err)
					}
					defer resp.Body.Close()
					var got struct {
						Status    string `json:"status"`
						ErrorType string `json:"errorType"`
						Error     string `json:"error"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
						t.Fatal(err)
					}
					if resp.StatusCode != wantStatus || got.ErrorType != wantType {
						t.Errorf("got HTTP %d %q, want HTTP %d %q (error: %s)", resp.StatusCode, got.ErrorType, wantStatus, wantType, got.Error)
					}
					if !strings.Contains(got.Error, tc.wantMsg) {
						t.Errorf("error %q does not contain %q", got.Error, tc.wantMsg)
					}
					if strings.Contains(got.Error, "ReadObject") {
						t.Errorf("error leaks JSON parser output: %q", got.Error)
					}
				})
			}
		})
	}
}
