package promclient

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

// logSeriesSet emits the trace/debug line for a SeriesSet-returning call.
func (d *DebugAPI) logSeriesSet(fields logrus.Fields, ss storage.SeriesSet) storage.SeriesSet {
	if logrus.GetLevel() > logrus.DebugLevel {
		fields["error"] = ss.Err()
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}
	return ss
}

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
func (d *DebugAPI) Query(ctx context.Context, query string, ts time.Time) storage.SeriesSet {
	fields := logrus.Fields{
		"api":   "Query",
		"query": query,
		"ts":    ts,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	ss := d.A.Query(ctx, query, ts)
	fields["took"] = time.Since(s)
	return d.logSeriesSet(fields, ss)
}

// QueryRange performs a query for the given range.
func (d *DebugAPI) QueryRange(ctx context.Context, query string, r v1.Range) storage.SeriesSet {
	fields := logrus.Fields{
		"api":   "QueryRange",
		"query": query,
		"r":     r,
	}
	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	ss := d.A.QueryRange(ctx, query, r)
	fields["took"] = time.Since(s)
	return d.logSeriesSet(fields, ss)
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
func (d *DebugAPI) GetValue(ctx context.Context, start, end time.Time, matchers []*labels.Matcher) storage.SeriesSet {
	fields := logrus.Fields{
		"api":      "GetValue",
		"start":    start,
		"end":      end,
		"matchers": matchers,
	}

	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	ss := d.A.GetValue(ctx, start, end, matchers)
	fields["took"] = time.Since(s)
	return d.logSeriesSet(fields, ss)
}

// Metadata returns metadata about metrics currently scraped by the metric name.
func (d *DebugAPI) Metadata(ctx context.Context, metric, limit string) (map[string][]v1.Metadata, error) {
	fields := logrus.Fields{
		"api":    "Metadata",
		"metric": metric,
		"limit":  limit,
	}

	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, err := d.A.Metadata(ctx, metric, limit)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, err
}

// QueryExemplars performs a query for exemplars by the given query and time range.
func (d *DebugAPI) QueryExemplars(ctx context.Context, query string, startTime, endTime time.Time) ([]v1.ExemplarQueryResult, error) {
	fields := logrus.Fields{
		"api":       "QueryExemplars",
		"query":     query,
		"startTime": startTime,
		"endTime":   endTime,
	}

	logrus.WithFields(fields).Debug(d.PrefixMessage)

	s := time.Now()
	v, err := d.A.QueryExemplars(ctx, query, startTime, endTime)
	fields["took"] = time.Since(s)

	if logrus.GetLevel() > logrus.DebugLevel {
		fields["value"] = v
		fields["error"] = err
		logrus.WithFields(fields).Trace(d.PrefixMessage)
	} else {
		logrus.WithFields(fields).Debug(d.PrefixMessage)
	}

	return v, err
}
