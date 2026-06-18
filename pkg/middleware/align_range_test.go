package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// epoch-grid (multiple of 7200s) anchors used across tests.
const (
	onGridStart = "1781654400" // 2026-06-17T00:00:00Z
	onGridEnd   = "1781697600" // 2026-06-17T12:00:00Z
	offGridStr  = "1781658000" // 2026-06-17T01:00:00Z (off grid by 1h)
	offGridEnd  = "1781701200" // 2026-06-17T13:00:00Z
)

func TestAlignRangeRequest(t *testing.T) {
	for _, tc := range []struct {
		name               string
		start, end, step   string
		wantStart, wantEnd string
		wantUntouched      bool
	}{
		{
			name:  "off-grid unix seconds floored down",
			start: offGridStr, end: offGridEnd, step: "7200",
			wantStart: onGridStart, wantEnd: onGridEnd,
		},
		{
			name:  "on-grid unchanged",
			start: onGridStart, end: onGridEnd, step: "7200",
			wantStart: onGridStart, wantEnd: onGridEnd,
		},
		{
			name:  "off-grid RFC3339 floored down",
			start: "2026-06-17T01:00:00Z", end: "2026-06-17T13:00:00Z", step: "7200",
			wantStart: onGridStart, wantEnd: onGridEnd,
		},
		{
			name:  "missing step is a no-op",
			start: offGridStr, end: offGridEnd, step: "",
			wantUntouched: true,
		},
		{
			name:  "zero step is a no-op",
			start: offGridStr, end: offGridEnd, step: "0",
			wantUntouched: true,
		},
		{
			name:  "unparsable start is a no-op",
			start: "not-a-time", end: offGridEnd, step: "7200",
			wantUntouched: true,
		},
		{
			name:  "unparsable end is a no-op",
			start: offGridStr, end: "not-a-time", step: "7200",
			wantUntouched: true,
		},
		{
			name:  "sub-millisecond step is a no-op",
			start: offGridStr, end: offGridEnd, step: "0.0005",
			wantUntouched: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := url.Values{}
			if tc.start != "" {
				v.Set("start", tc.start)
			}
			if tc.end != "" {
				v.Set("end", tc.end)
			}
			if tc.step != "" {
				v.Set("step", tc.step)
			}
			req := httptest.NewRequest("GET", "/api/v1/query_range?"+v.Encode(), nil)

			alignRangeRequest(req)

			gotStart, gotEnd := req.Form.Get("start"), req.Form.Get("end")
			if tc.wantUntouched {
				if gotStart != tc.start || gotEnd != tc.end {
					t.Fatalf("expected no rewrite, got start=%q end=%q (was start=%q end=%q)", gotStart, gotEnd, tc.start, tc.end)
				}
				return
			}

			gotStartT, err := parseQueryTime(gotStart)
			if err != nil {
				t.Fatalf("rewritten start %q unparsable: %v", gotStart, err)
			}
			gotEndT, err := parseQueryTime(gotEnd)
			if err != nil {
				t.Fatalf("rewritten end %q unparsable: %v", gotEnd, err)
			}
			wantStartT, _ := parseQueryTime(tc.wantStart)
			wantEndT, _ := parseQueryTime(tc.wantEnd)
			if !gotStartT.Equal(wantStartT) {
				t.Errorf("start: got %v, want %v", gotStartT.UTC(), wantStartT.UTC())
			}
			if !gotEndT.Equal(wantEndT) {
				t.Errorf("end: got %v, want %v", gotEndT.UTC(), wantEndT.UTC())
			}
			const stepMs = int64(7200 * 1000)
			if gotStartT.UnixMilli()%stepMs != 0 || gotEndT.UnixMilli()%stepMs != 0 {
				t.Errorf("result not aligned to step: start=%v end=%v", gotStartT.UTC(), gotEndT.UTC())
			}
		})
	}
}

// TestAlignRangeRequestPOST ensures the form-body (POST) path is rewritten too, not just the URL query (GET) path.
func TestAlignRangeRequestPOST(t *testing.T) {
	body := url.Values{"start": {offGridStr}, "end": {offGridEnd}, "step": {"7200"}}.Encode()
	req := httptest.NewRequest("POST", "/api/v1/query_range", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	alignRangeRequest(req)

	if got := req.Form.Get("start"); got != onGridStart {
		t.Errorf("start: got %q, want %q", got, onGridStart)
	}
	if got := req.Form.Get("end"); got != onGridEnd {
		t.Errorf("end: got %q, want %q", got, onGridEnd)
	}
}

// TestAlignRangeRequestParseFormError covers the ParseForm error path (malformed query string):
// the function must return without panicking and without rewriting anything.
func TestAlignRangeRequestParseFormError(t *testing.T) {
	req := httptest.NewRequest("GET", "/api/v1/query_range", nil)
	req.URL.RawQuery = "%zz" // invalid percent-encoding -> ParseForm errors
	alignRangeRequest(req)
	if got := req.Form.Get("start"); got != "" {
		t.Errorf("expected no rewrite on ParseForm error, got start=%q", got)
	}
}

func TestAlignQueryRangeStepServeHTTP(t *testing.T) {
	var called bool
	var gotStart string
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_ = r.ParseForm()
		gotStart = r.Form.Get("start")
	})
	mw := NewAlignQueryRangeStep(inner, "/api/v1/query_range")

	// Matching path -> start aligned before reaching inner handler.
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/query_range?start="+offGridStr+"&end="+offGridEnd+"&step=7200", nil))
	if !called {
		t.Fatal("inner handler not called")
	}
	if gotStart != onGridStart {
		t.Errorf("matching path: got start=%q, want %q", gotStart, onGridStart)
	}

	// Non-matching path -> passed through untouched.
	called, gotStart = false, ""
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/api/v1/query?start="+offGridStr, nil))
	if !called {
		t.Fatal("inner handler not called on passthrough")
	}
	if gotStart != offGridStr {
		t.Errorf("passthrough modified start: got %q, want %q", gotStart, offGridStr)
	}
}

func TestFloorToStep(t *testing.T) {
	for _, tc := range []struct {
		ts, step, want int64
	}{
		{ts: 3600_000, step: 7200_000, want: 0},
		{ts: 7200_000, step: 7200_000, want: 7200_000},
		{ts: 10800_000, step: 7200_000, want: 7200_000},
		{ts: 0, step: 7200_000, want: 0},
	} {
		if got := floorToStep(tc.ts, tc.step); got != tc.want {
			t.Errorf("floorToStep(%d,%d)=%d, want %d", tc.ts, tc.step, got, tc.want)
		}
	}
}

func TestParseQueryTime(t *testing.T) {
	for _, tc := range []struct {
		in      string
		wantSec int64
		wantErr bool
	}{
		{in: "1781654400", wantSec: 1781654400},
		{in: "1781654400.000", wantSec: 1781654400},
		{in: "2026-06-17T00:00:00Z", wantSec: 1781654400},
		{in: "2026-06-17T00:00:00.000Z", wantSec: 1781654400},
		{in: "not-a-time", wantErr: true},
		{in: "", wantErr: true},
	} {
		got, err := parseQueryTime(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseQueryTime(%q): expected error, got %v", tc.in, got.UTC())
			}
			continue
		}
		if err != nil {
			t.Errorf("parseQueryTime(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got.Unix() != tc.wantSec {
			t.Errorf("parseQueryTime(%q): got %d, want %d", tc.in, got.Unix(), tc.wantSec)
		}
	}
}

func TestParseQueryDuration(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "7200", want: 2 * time.Hour},
		{in: "120.5", want: 120500 * time.Millisecond},
		{in: "2h", want: 2 * time.Hour},
		{in: "5m", want: 5 * time.Minute},
		{in: "garbage", wantErr: true},
		{in: "1e19", wantErr: true}, // overflows int64 nanoseconds
	} {
		got, err := parseQueryDuration(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("parseQueryDuration(%q): expected error, got %v", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseQueryDuration(%q): unexpected error %v", tc.in, err)
			continue
		}
		if got != tc.want {
			t.Errorf("parseQueryDuration(%q): got %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestFormatUnixMillis(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want string
	}{
		{ms: 1781654400000, want: "1781654400"},
		{ms: 1781654400500, want: "1781654400.5"},
		{ms: 0, want: "0"},
	} {
		if got := formatUnixMillis(tc.ms); got != tc.want {
			t.Errorf("formatUnixMillis(%d): got %q, want %q", tc.ms, got, tc.want)
		}
	}
}
