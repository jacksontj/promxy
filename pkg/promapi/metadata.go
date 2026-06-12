package promapi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/util/annotations"
)

// Metadata is a single metric metadata entry.
type Metadata struct {
	Type string `json:"type"`
	Help string `json:"help"`
	Unit string `json:"unit"`
}

// get issues a GET request and decodes the response envelope, returning the raw
// `data` field plus warnings/infos and any API error.
func (c *Client) get(ctx context.Context, ep string, args url.Values) ([]byte, annotations.Annotations, error) {
	u := *c.URL
	u.Path = path.Join(u.Path, ep)
	u.RawQuery = args.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, nil, err
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, err
	}
	return decodeEnvelope(body)
}

// decodeEnvelope pulls `data` (raw), warnings, infos and any status:"error" out
// of a response body.
func decodeEnvelope(body []byte) ([]byte, annotations.Annotations, error) {
	iter := jsonCfg.BorrowIterator(body)
	defer jsonCfg.ReturnIterator(iter)
	var (
		status, errType, errMsg string
		data                    []byte
		anns                    annotations.Annotations
	)
	for key := iter.ReadObject(); key != ""; key = iter.ReadObject() {
		switch key {
		case "status":
			status = iter.ReadString()
		case "errorType":
			errType = iter.ReadString()
		case "error":
			errMsg = iter.ReadString()
		case "warnings", "infos":
			for iter.ReadArray() {
				anns = anns.Add(toAnnotationError(iter.ReadString()))
			}
		case "data":
			data = iter.SkipAndReturnBytes()
		default:
			iter.Skip()
		}
	}
	if iter.Error != nil && !errors.Is(iter.Error, io.EOF) {
		return nil, anns, &ResponseError{Type: "bad_response", Msg: iter.Error.Error()}
	}
	if status == "error" {
		return nil, anns, &ResponseError{Type: errType, Msg: errMsg}
	}
	return data, anns, nil
}

func matchTimeArgs(args url.Values, matchers []*labels.Matcher, start, end time.Time) {
	if len(matchers) > 0 {
		args.Set("match[]", matchersToSelector(matchers))
	}
	if !start.IsZero() {
		args.Set("start", formatTime(start))
	}
	if !end.IsZero() {
		args.Set("end", formatTime(end))
	}
}

// LabelNames returns label names matching the matchers over [start, end].
func (c *Client) LabelNames(ctx context.Context, matchers []*labels.Matcher, start, end time.Time) ([]string, annotations.Annotations, error) {
	args := url.Values{}
	matchTimeArgs(args, matchers, start, end)
	data, anns, err := c.get(ctx, "/api/v1/labels", args)
	if err != nil {
		return nil, anns, err
	}
	var out []string
	if len(data) > 0 {
		err = jsonCfg.Unmarshal(data, &out)
	}
	return out, anns, err
}

// LabelValues returns the values of label matching the matchers over [start, end].
func (c *Client) LabelValues(ctx context.Context, label string, matchers []*labels.Matcher, start, end time.Time) ([]string, annotations.Annotations, error) {
	args := url.Values{}
	matchTimeArgs(args, matchers, start, end)
	data, anns, err := c.get(ctx, "/api/v1/label/"+label+"/values", args)
	if err != nil {
		return nil, anns, err
	}
	var out []string
	if len(data) > 0 {
		err = jsonCfg.Unmarshal(data, &out)
	}
	return out, anns, err
}

// Series returns the label sets of series matching the matchers over [start, end].
func (c *Client) Series(ctx context.Context, matchers []*labels.Matcher, start, end time.Time) ([]labels.Labels, annotations.Annotations, error) {
	args := url.Values{}
	matchTimeArgs(args, matchers, start, end)
	data, anns, err := c.get(ctx, "/api/v1/series", args)
	if err != nil || len(data) == 0 {
		return nil, anns, err
	}
	iter := jsonCfg.BorrowIterator(data)
	defer jsonCfg.ReturnIterator(iter)
	var out []labels.Labels
	var b labels.ScratchBuilder
	for iter.ReadArray() {
		b.Reset()
		readMetric(iter, &b)
		b.Sort()
		out = append(out, b.Labels())
	}
	if iter.Error != nil && !errors.Is(iter.Error, io.EOF) {
		return out, anns, iter.Error
	}
	return out, anns, nil
}

// Metadata returns metric metadata, optionally limited to a metric and/or count.
func (c *Client) Metadata(ctx context.Context, metric, limit string) (map[string][]Metadata, error) {
	args := url.Values{}
	if metric != "" {
		args.Set("metric", metric)
	}
	if limit != "" {
		if _, err := strconv.Atoi(limit); err == nil {
			args.Set("limit", limit)
		}
	}
	data, _, err := c.get(ctx, "/api/v1/metadata", args)
	if err != nil {
		return nil, err
	}
	out := map[string][]Metadata{}
	if len(data) > 0 {
		err = jsonCfg.Unmarshal(data, &out)
	}
	return out, err
}
