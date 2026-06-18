package middleware

import (
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/prometheus/common/model"
)

// NewAlignQueryRangeStep wraps h and, for requests to queryRangePath, snaps the query_range
// start/end down to a multiple of step before they reach the wrapped handler. This keeps Promxy's
// local evaluation grid on the epoch step grid, matching backends that step-align query_range
// results (e.g. Mimir/Cortex); without it an unaligned start (start % step != 0) yields no data.
func NewAlignQueryRangeStep(h http.Handler, queryRangePath string) *AlignQueryRangeStep {
	return &AlignQueryRangeStep{h: h, path: queryRangePath}
}

type AlignQueryRangeStep struct {
	h    http.Handler
	path string
}

func (a *AlignQueryRangeStep) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == a.path {
		alignRangeRequest(r)
	}
	a.h.ServeHTTP(w, r)
}

// alignRangeRequest rewrites the request form in place. It is a no-op if start/end/step are
// missing or unparsable, or if the range is already aligned.
func alignRangeRequest(r *http.Request) {
	if err := r.ParseForm(); err != nil {
		return
	}
	stepStr, startStr, endStr := r.Form.Get("step"), r.Form.Get("start"), r.Form.Get("end")
	if stepStr == "" || startStr == "" || endStr == "" {
		return
	}
	step, err := parseQueryDuration(stepStr)
	if err != nil || step <= 0 {
		return
	}
	start, err := parseQueryTime(startStr)
	if err != nil {
		return
	}
	end, err := parseQueryTime(endStr)
	if err != nil {
		return
	}
	stepMs := step.Milliseconds()
	if stepMs <= 0 {
		return
	}
	r.Form.Set("start", formatUnixMillis(floorToStep(start.UnixMilli(), stepMs)))
	r.Form.Set("end", formatUnixMillis(floorToStep(end.UnixMilli(), stepMs)))
}

// floorToStep snaps a millisecond timestamp down to a multiple of step. This mirrors the
// algorithm used by Mimir's query-frontend step alignment, itself from Cortex (Apache-2.0):
// start := (start / step) * step. It is reimplemented here rather than imported to avoid pulling
// in Mimir (AGPL-3.0, and a conflicting prometheus fork). Query timestamps are non-negative, so
// integer truncation toward zero is equivalent to floor.
func floorToStep(tsMs, stepMs int64) int64 {
	return (tsMs / stepMs) * stepMs
}

func formatUnixMillis(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
}

// parseQueryTime and parseQueryDuration mirror the parsing in prometheus/web/api/v1 so the
// rewritten values are interpreted identically by the downstream handler.
func parseQueryTime(s string) (time.Time, error) {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		sec, frac := math.Modf(v)
		frac = math.Round(frac*1000) / 1000
		return time.Unix(int64(sec), int64(frac*float64(time.Second))).UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Time{}, fmt.Errorf("cannot parse %q to a valid timestamp", s)
}

func parseQueryDuration(s string) (time.Duration, error) {
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		ts := v * float64(time.Second)
		if ts > float64(math.MaxInt64) || ts < float64(math.MinInt64) {
			return 0, fmt.Errorf("cannot parse %q to a valid duration. It overflows int64", s)
		}
		return time.Duration(ts), nil
	}
	if d, err := model.ParseDuration(s); err == nil {
		return time.Duration(d), nil
	}
	return 0, fmt.Errorf("cannot parse %q to a valid duration", s)
}
