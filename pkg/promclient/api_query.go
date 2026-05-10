package promclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

// The v1 client only parses the `warnings` field of the JSON response. With
// Prometheus 3.x there is also an `infos` field for info-level annotations
// (rate() on a non-_total metric, histogram quantile monotonicity fixes,
// etc.). Those are dropped by the client. promxy needs them to be visible
// when pushing down @-bearing subtrees, otherwise eval_info expectations
// regress for any Call we push down.
//
// queryWithInfos issues the same request the v1 client would, but parses
// both `warnings` and `infos`. The two are concatenated into v1.Warnings
// preserving their "PromQL info: " / "PromQL warning: " prefixes — the
// upstream's annotations.AsStrings serialization always emits the prefix —
// so promhttputil.WarningsConvert can re-wrap them with PromQLInfo /
// PromQLWarning at the consumer.

const (
	epQuery      = "/api/v1/query"
	epQueryRange = "/api/v1/query_range"
)

type apiResponseWithInfos struct {
	Status    string          `json:"status"`
	Data      json.RawMessage `json:"data"`
	ErrorType v1.ErrorType    `json:"errorType"`
	Error     string          `json:"error"`
	Warnings  []string        `json:"warnings,omitempty"`
	Infos     []string        `json:"infos,omitempty"`
}

// queryResult mirrors the unexported struct in client_golang's v1 package.
type queryResult struct {
	v model.Value
}

func (qr *queryResult) UnmarshalJSON(b []byte) error {
	v := struct {
		Type   model.ValueType `json:"resultType"`
		Result json.RawMessage `json:"result"`
	}{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	switch v.Type {
	case model.ValScalar:
		var sv model.Scalar
		if err := json.Unmarshal(v.Result, &sv); err != nil {
			return err
		}
		qr.v = &sv
	case model.ValVector:
		var vv model.Vector
		if err := json.Unmarshal(v.Result, &vv); err != nil {
			return err
		}
		qr.v = vv
	case model.ValMatrix:
		var mv model.Matrix
		if err := json.Unmarshal(v.Result, &mv); err != nil {
			return err
		}
		qr.v = mv
	case model.ValString:
		var s model.String
		if err := json.Unmarshal(v.Result, &s); err != nil {
			return err
		}
		qr.v = &s
	default:
		return fmt.Errorf("unknown result type %q", v.Type)
	}
	return nil
}

func formatAPITime(t time.Time) string {
	return strconv.FormatFloat(float64(t.Unix())+float64(t.Nanosecond())/1e9, 'f', -1, 64)
}

// hasNegativeFractionalSecond reports whether t falls strictly before the
// epoch and is not aligned to a whole second. The upstream
// prometheus/common (v0.67.5 latest as of writing) model.Time.UnmarshalJSON
// mis-decodes such timestamps when they appear in API responses: for the
// JSON literal "-59.200" it returns Time(-58800) instead of Time(-59200)
// because the negative sign on the integer part is not propagated to the
// fractional part. The bug is harmless in production (Unix timestamps are
// non-negative) but corrupts pushdown results when a query's range spans
// pre-epoch seconds with sub-second precision (only seen via the upstream
// promqltest's @-modifier multi-evalTime sweep). We refuse the pushdown
// rather than silently returning wrong data.
func hasNegativeFractionalSecond(t time.Time) bool {
	if t.Unix() >= 0 && t.Nanosecond() == 0 {
		return false
	}
	return t.Unix() < 0 && t.Nanosecond() != 0
}

// errNegativeFractionalTimestamp is returned by Query / QueryRange when the
// requested time would trigger the upstream JSON decoding bug described on
// hasNegativeFractionalSecond. It surfaces explicitly to the caller so the
// failure mode is observable rather than producing silently shifted data.
var errNegativeFractionalTimestamp = errors.New("promxy: pushdown not supported for pre-epoch timestamps with sub-second precision (https://github.com/prometheus/common model.Time.UnmarshalJSON bug)")

func queryWithInfos(ctx context.Context, c api.Client, u *url.URL, args url.Values) (model.Value, v1.Warnings, error) {
	encoded := args.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), strings.NewReader(encoded))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, body, err := c.Do(ctx, req)
	if err != nil {
		return nil, nil, err
	}

	code := resp.StatusCode
	var ar apiResponseWithInfos
	if code != http.StatusNoContent {
		if jsonErr := json.Unmarshal(body, &ar); jsonErr != nil {
			return nil, nil, &v1.Error{Type: v1.ErrBadResponse, Msg: jsonErr.Error()}
		}
	}

	combined := make(v1.Warnings, 0, len(ar.Warnings)+len(ar.Infos))
	combined = append(combined, ar.Warnings...)
	combined = append(combined, ar.Infos...)

	if ar.Status == "error" {
		return nil, combined, &v1.Error{Type: ar.ErrorType, Msg: ar.Error}
	}
	if code/100 != 2 {
		return nil, combined, &v1.Error{Type: v1.ErrBadResponse, Msg: fmt.Sprintf("bad response code: %d", code)}
	}

	var qres queryResult
	if len(ar.Data) > 0 {
		if err := json.Unmarshal(ar.Data, &qres); err != nil {
			return nil, combined, &v1.Error{Type: v1.ErrBadResponse, Msg: err.Error()}
		}
	}
	return qres.v, combined, nil
}
