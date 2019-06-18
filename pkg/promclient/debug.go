package promclient

import (
	"context"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/sirupsen/logrus"
)

// DebugAPI simply logs debug lines for the given API with the given prefix
type DebugAPI struct {
	API
	PrefixMessage string
}

// LabelValues performs a query for the values of the given label.
func (d *DebugAPI) LabelValues(ctx context.Context, label string) (model.LabelValues, api.Warnings, error) {
	logrus.WithFields(logrus.Fields{
		"api":   "LabelValues",
		"label": label,
	}).Debug(d.PrefixMessage)

	v, w, err := d.API.LabelValues(ctx, label)

	logrus.WithFields(logrus.Fields{
		"api":      "LabelValues",
		"label":    label,
		"value":    v,
		"warnings": w,
		"error":    err,
	}).Trace(d.PrefixMessage)

	return v, w, err
}

// Query performs a query for the given time.
func (d *DebugAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, api.Warnings, error) {
	logrus.WithFields(logrus.Fields{
		"api":   "Query",
		"query": query,
		"ts":    ts,
	}).Debug(d.PrefixMessage)

	v, w, err := d.API.Query(ctx, query, ts)

	logrus.WithFields(logrus.Fields{
		"api":      "Query",
		"query":    query,
		"ts":       ts,
		"value":    v,
		"warnings": w,
		"error":    err,
	}).Trace(d.PrefixMessage)

	return v, w, err
}

// QueryRange performs a query for the given range.
func (d *DebugAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, api.Warnings, error) {
	fmt.Println("what")
	logrus.WithFields(logrus.Fields{
		"api":   "QueryRange",
		"query": query,
		"r":     r,
	}).Debug(d.PrefixMessage)

	v, w, err := d.API.QueryRange(ctx, query, r)

	logrus.WithFields(logrus.Fields{
		"api":      "QueryRange",
		"query":    query,
		"r":        r,
		"value":    v,
		"warnings": w,
		"error":    err,
	}).Trace(d.PrefixMessage)

	return v, w, err
}

// Series finds series by label matchers.
func (d *DebugAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, api.Warnings, error) {
	logrus.WithFields(logrus.Fields{
		"api":       "Series",
		"matches":   matches,
		"startTime": startTime,
		"endTime":   endTime,
	}).Debug(d.PrefixMessage)

	v, w, err := d.API.Series(ctx, matches, startTime, endTime)

	logrus.WithFields(logrus.Fields{
		"api":       "Series",
		"matches":   matches,
		"startTime": startTime,
		"endTime":   endTime,
		"value":     v,
		"warnings":  w,
		"error":     err,
	}).Trace(d.PrefixMessage)

	return v, w, err
}

// GetValue loads the raw data for a given set of matchers in the time range
func (d *DebugAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, api.Warnings, error) {
	logrus.WithFields(logrus.Fields{
		"api":      "GetValue",
		"start":    start,
		"end":      end,
		"matchers": matchers,
	}).Debug(d.PrefixMessage)

	v, w, err := d.API.GetValue(ctx, start, end, matchers)

	logrus.WithFields(logrus.Fields{
		"api":      "GetValue",
		"start":    start,
		"end":      end,
		"matchers": matchers,
		"value":    v,
		"warnings": w,
		"error":    err,
	}).Trace(d.PrefixMessage)

	return v, w, err
}
