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

// TestDownstreamErrors runs promxy's full HTTP API in front of backends that
// fail, and checks what the client of promxy sees: the HTTP status and the
// error text. The backend failure has to survive the whole chain (decode,
// target ErrorWrap, server group MultiAPI, servergroup ErrorWrap, the
// ProxyStorage MultiAPI, the engine and the API's error mapping).
func TestDownstreamErrors(t *testing.T) {
	respond := func(status int, contentType, body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", contentType)
			w.WriteHeader(status)
			io.WriteString(w, body)
		}
	}
	jsonError := func(status int, errType, msg string) http.HandlerFunc {
		return respond(status, "application/json", fmt.Sprintf(`{"status":"error","errorType":%q,"error":%q}`, errType, msg))
	}
	// hang never answers, so a server group timeout is the only way the
	// request ends. The server only notices the client hanging up once the
	// body has been read.
	hang := func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		<-r.Context().Done()
	}
	refused := http.HandlerFunc(nil)

	var (
		execution   = jsonError(http.StatusUnprocessableEntity, "execution", "many-to-many matching not allowed")
		notReady    = respond(http.StatusServiceUnavailable, "text/plain", "too many outstanding requests\n")
		promxyBelow = jsonError(http.StatusInternalServerError, "internal", "error in target=http://deeper: connection refused")
	)

	cases := []struct {
		name string
		// backends are the server group's targets; a nil one refuses
		// connections.
		backends []http.HandlerFunc
		// timeout is the server group's timeout, if any.
		timeout string
		// wantStatus/wantType are promxy's answer.
		wantStatus int
		wantType   string
		// wantMsg must appear in promxy's error message.
		wantMsg string
	}{
		{
			name:       "json_timeout_envelope",
			backends:   []http.HandlerFunc{jsonError(http.StatusServiceUnavailable, "timeout", "query timed out in expression evaluation")},
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "timeout",
			wantMsg:    "query timed out in expression evaluation",
		},
		{
			name:       "json_canceled_envelope",
			backends:   []http.HandlerFunc{jsonError(http.StatusServiceUnavailable, "canceled", "query was canceled in expression evaluation")},
			wantStatus: 499, // the vendored API's statusClientClosedConnection
			wantType:   "canceled",
			wantMsg:    "query was canceled in expression evaluation",
		},
		{
			name:       "json_execution_envelope",
			backends:   []http.HandlerFunc{execution},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "many-to-many matching not allowed",
		},
		{
			name:       "json_bad_data_envelope",
			backends:   []http.HandlerFunc{jsonError(http.StatusBadRequest, "bad_data", "invalid parameter")},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "invalid parameter",
		},
		{
			// promxy in front of another promxy whose own backend is down.
			name:       "json_internal_envelope",
			backends:   []http.HandlerFunc{promxyBelow},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "error in target=http://deeper: connection refused",
		},
		{
			name:       "plain_text_503",
			backends:   []http.HandlerFunc{notReady},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "too many outstanding requests",
		},
		{
			name:       "plain_text_429",
			backends:   []http.HandlerFunc{respond(http.StatusTooManyRequests, "text/plain", "rate limited")},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "rate limited",
		},
		{
			name:       "html_502",
			backends:   []http.HandlerFunc{respond(http.StatusBadGateway, "text/html", "<html><body>502 Bad Gateway</body></html>")},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "502 Bad Gateway",
		},
		{
			name:       "plain_text_401",
			backends:   []http.HandlerFunc{respond(http.StatusUnauthorized, "text/plain", "unauthorized")},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "unauthorized",
		},
		{
			name:       "connection_refused",
			backends:   []http.HandlerFunc{refused},
			wantStatus: http.StatusInternalServerError,
			wantType:   "internal",
			wantMsg:    "connection refused",
		},
		{
			name:       "server_group_timeout",
			backends:   []http.HandlerFunc{hang},
			timeout:    "100ms",
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "timeout",
			wantMsg:    "timeout awaiting response headers",
		},
		{
			// Every replica fails, differently: a query one replica refuses,
			// every replica refuses, so the refusal is the answer, whichever
			// replica is listed (or answers) first.
			name:       "replicas_refused_and_down",
			backends:   []http.HandlerFunc{notReady, refused, execution},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "many-to-many matching not allowed",
		},
		{
			name:       "replicas_down_and_refused",
			backends:   []http.HandlerFunc{execution, refused, notReady},
			wantStatus: http.StatusUnprocessableEntity,
			wantType:   "execution",
			wantMsg:    "many-to-many matching not allowed",
		},
		{
			name:       "replicas_timeout_and_down",
			backends:   []http.HandlerFunc{notReady, hang},
			timeout:    "100ms",
			wantStatus: http.StatusServiceUnavailable,
			wantType:   "timeout",
			wantMsg:    "timeout awaiting response headers",
		},
	}

	endpoints := []struct {
		path string
		// alwaysExec is set for the endpoints whose vendored Prometheus
		// handler answers every storage error as 422 execution, without
		// looking at the error's class. Changing that takes a change to the
		// Prometheus fork.
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
			cfg := "promxy:\n  server_groups:\n    - static_configs:\n        - targets:\n"
			for _, h := range tc.backends {
				backendSrv := httptest.NewServer(h)
				if h == nil {
					// Closed: its address now refuses connections.
					backendSrv.Close()
				} else {
					defer backendSrv.Close()
				}
				cfg += "          - " + strings.TrimPrefix(backendSrv.URL, "http://") + "\n"
			}
			if tc.timeout != "" {
				cfg += "      timeout: " + tc.timeout + "\n"
			}

			ps := getProxyStorage(cfg)
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
