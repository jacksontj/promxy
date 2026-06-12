// Package promapi is a small, standalone Prometheus HTTP API query client that
// decodes responses straight into storage.SeriesSet (via DecodeSeriesSet),
// avoiding client_golang's model.Value intermediate. It depends only on
// prometheus's storage/labels/model packages, so it is reusable on its own and
// composes with the prometheus storage ecosystem.
package promapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
)

// Range is the [start, end] window with a resolution step for QueryRange.
type Range struct {
	Start time.Time
	End   time.Time
	Step  time.Duration
}

// Client queries a single Prometheus-compatible HTTP API. The transport
// (TLS, auth, timeouts, ...) is supplied via HTTPClient, so callers control
// exactly how requests are made.
type Client struct {
	// URL is the base URL of the server (e.g. https://prom:9090).
	URL *url.URL
	// HTTPClient is the transport; nil uses http.DefaultClient.
	HTTPClient *http.Client
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// post issues a POST form request to the given endpoint and returns the body.
// Non-transport failures (including HTTP 4xx/5xx) still return the body so the
// JSON {"status":"error",...} envelope can surface the real API error.
func (c *Client) post(ctx context.Context, ep string, args url.Values) ([]byte, error) {
	u := *c.URL
	u.Path = path.Join(u.Path, ep)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(args.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// Query runs an instant query at ts (zero ts means "now", server-evaluated).
func (c *Client) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	args := url.Values{}
	args.Set("query", query)
	if !ts.IsZero() {
		args.Set("time", formatTime(ts))
	}
	body, err := c.post(ctx, "/api/v1/query", args)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return DecodeSeriesSet(body)
}

// QueryRange runs a range query.
func (c *Client) QueryRange(ctx context.Context, query string, r Range) storage.SeriesSet {
	args := url.Values{}
	args.Set("query", query)
	args.Set("start", formatTime(r.Start))
	args.Set("end", formatTime(r.End))
	args.Set("step", strconv.FormatFloat(r.Step.Seconds(), 'f', -1, 64))
	body, err := c.post(ctx, "/api/v1/query_range", args)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	return DecodeSeriesSet(body)
}

// Select fetches the raw series matching the matchers over [start, end]. It is
// implemented as an instant query over a range selector, returning every stored
// sample in the window (the shape promxy's storage layer needs).
func (c *Client) Select(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) storage.SeriesSet {
	seconds := int64(end.Sub(start).Seconds()) + 1
	query := matchersToSelector(matchers) + "[" + strconv.FormatInt(seconds, 10) + "s]"
	return c.Query(ctx, query, end)
}

func formatTime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}

func matchersToSelector(matchers []*labels.Matcher) string {
	if len(matchers) == 0 {
		return "{}"
	}
	parts := make([]string, len(matchers))
	for i, m := range matchers {
		parts[i] = m.String()
	}
	return "{" + strings.Join(parts, ",") + "}"
}
