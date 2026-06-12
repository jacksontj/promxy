package promclient

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	"github.com/prometheus/prometheus/storage"

	"github.com/jacksontj/promxy/pkg/promapi"
)

const (
	epQuery      = "/api/v1/query"
	epQueryRange = "/api/v1/query_range"
)

func formatAPITime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}

// hasNegativeFractionalSecond reports whether t falls strictly before the
// epoch and is not aligned to a whole second. The upstream prometheus/common
// model.Time.UnmarshalJSON mis-decodes such timestamps in API responses: for
// "-59.200" it returns Time(-58800) instead of Time(-59200). The bug is
// harmless in production (Unix timestamps are non-negative) but corrupts
// pushdown results when a query's range spans pre-epoch seconds with sub-second
// precision. We refuse the pushdown rather than silently returning wrong data.
func hasNegativeFractionalSecond(t time.Time) bool {
	if t.Unix() >= 0 && t.Nanosecond() == 0 {
		return false
	}
	return t.Unix() < 0 && t.Nanosecond() != 0
}

// errNegativeFractionalTimestamp is returned when the requested time would
// trigger the upstream JSON decoding bug described on hasNegativeFractionalSecond.
var errNegativeFractionalTimestamp = errors.New("promxy: pushdown not supported for pre-epoch timestamps with sub-second precision (https://github.com/prometheus/common model.Time.UnmarshalJSON bug)")

// queryWithInfos issues the query/query_range request over the given transport
// and decodes the response straight into a storage.SeriesSet. Warnings and
// infos ride in SeriesSet.Warnings(); errors in SeriesSet.Err(). It captures
// both `warnings` and `infos` (the v1 client drops the latter) because
// DecodeSeriesSet reads both fields.
func queryWithInfos(ctx context.Context, c api.Client, u *url.URL, args url.Values) storage.SeriesSet {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(args.Encode()))
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, body, err := c.Do(ctx, req)
	if err != nil {
		return storage.ErrSeriesSet(err)
	}
	if resp.StatusCode == http.StatusNoContent {
		return promapi.NewSeriesSet(nil, nil, nil)
	}
	return promapi.DecodeSeriesSet(body)
}
