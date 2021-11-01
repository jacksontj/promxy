package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/sirupsen/logrus"
)

// DebugAPI simply logs debug lines for the given API with the given prefix
type DebugAPI struct {
	A             API
	PrefixMessage string
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (d *DebugAPI) LabelNames(ctx context.Context, matchers []string, startTime time.Time, endTime time.Time) ([]string, v1.Warnings, error) {
	fields := logrus.Fields{
		"api": "LabelNames",
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.LabelNames(ctx, matchers, startTime, endTime)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, w, err
}

// LabelValues performs a query for the values of the given label.
func (d *DebugAPI) LabelValues(ctx context.Context, label string, matchers []string, startTime time.Time, endTime time.Time) (model.LabelValues, v1.Warnings, error) {
	fields := logrus.Fields{
		"api":   "LabelValues",
		"label": label,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.LabelValues(ctx, label, matchers, startTime, endTime)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, w, err
}

// Query performs a query for the given time.
func (d *DebugAPI) Query(ctx context.Context, query string, ts time.Time) (model.Value, v1.Warnings, error) {
	fields := logrus.Fields{
		"api":   "Query",
		"query": query,
		"ts":    ts,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.Query(ctx, query, ts)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, w, err
}

// QueryRange performs a query for the given range.
func (d *DebugAPI) QueryRange(ctx context.Context, query string, r v1.Range) (model.Value, v1.Warnings, error) {
	fields := logrus.Fields{
		"api":   "QueryRange",
		"query": query,
		"r":     r,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.QueryRange(ctx, query, r)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, w, err
}

// Series finds series by label matchers.
func (d *DebugAPI) Series(ctx context.Context, matches []string, startTime time.Time, endTime time.Time) ([]model.LabelSet, v1.Warnings, error) {
	fields := logrus.Fields{
		"api":       "Series",
		"matches":   matches,
		"startTime": startTime,
		"endTime":   endTime,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.Series(ctx, matches, startTime, endTime)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}
	return v, w, err
}

// GetValue loads the raw data for a given set of matchers in the time range
func (d *DebugAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) (model.Value, v1.Warnings, error) {
	fields := logrus.Fields{
		"api":      "GetValue",
		"start":    start,
		"end":      end,
		"matchers": matchers,
	}

	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, w, err := d.A.GetValue(ctx, start, end, matchers)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["warnings"] = w
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, w, err
}
