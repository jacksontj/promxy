package proxyquerier

import (
	"context"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/timestamp"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"
	"github.com/sirupsen/logrus"

	proxyconfig "github.com/jacksontj/promxy/pkg/config"
	"github.com/jacksontj/promxy/pkg/promclient"
	"github.com/jacksontj/promxy/pkg/promhttputil"
)

// ProxyQuerier Implements prometheus' Querier interface
type ProxyQuerier struct {
	Ctx    context.Context
	Start  time.Time
	End    time.Time
	Client promclient.API

	Cfg *proxyconfig.PromxyConfig
}

// Select returns a set of series that matches the given label matchers.
// TODO: switch based on sortSeries bool(first arg)
func (h *ProxyQuerier) Select(ctx context.Context, _ bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"selectHints": hints,
			"matchers":    matchers,
			"took":        time.Since(start),
		}).Debug("Select")
	}()

	var result model.Value
	var warnings annotations.Annotations
	var err error
	// Select() is a combined API call for query/query_range/series.
	// as of right now there is no great way of differentiating between a
	// data call (query/query_range) and a metadata call (series). For now
	// the working workaround is to switch based on the hints.
	// https://github.com/prometheus/prometheus/issues/4057
	if hints == nil || hints.Func == "series" {
		matcherString, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return NewSeriesSet(nil, nil, err)
		}
		labelsets, w, err := h.Client.Series(ctx, []string{matcherString}, h.Start, h.End)
		warnings = promhttputil.WarningsConvert(w)
		if err != nil {
			return NewSeriesSet(nil, warnings, err)
		}
		// Convert labelsets to vectors
		// convert to vector (there aren't points, but this way we don't have to make more merging functions)
		retVector := make(model.Vector, len(labelsets))
		for j, labelset := range labelsets {
			retVector[j] = &model.Sample{
				Metric: model.Metric(labelset),
			}
		}
		result = retVector
	} else {
		var w v1.Warnings
		t1 := timestamp.Time(hints.Start)
		t2 := timestamp.Time(hints.End)
		result, w, err = h.Client.GetValue(ctx, t1, t2, matchers)
		warnings = promhttputil.WarningsConvert(w)
	}
	if err != nil {
		return NewSeriesSet(nil, warnings, err)
	}

	iterators := promclient.IteratorsForValue(result)

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &Series{iterator}
	}

	return NewSeriesSet(series, warnings, nil)
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(ctx context.Context, name string, hints *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name":     name,
			"matchers": matchers,
			"took":     time.Since(start),
		}).Debug("LabelValues")
	}()

	var matchersStrings []string
	if len(matchers) > 0 {
		s, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return nil, nil, err
		}
		matchersStrings = []string{s}
	}

	result, w, err := h.Client.LabelValues(ctx, name, matchersStrings, h.Start, h.End)
	warnings := promhttputil.WarningsConvert(w)
	if err != nil {
		return nil, warnings, err
	}

	ret := make([]string, len(result))
	for i, r := range result {
		ret[i] = string(r)
	}

	return ret, warnings, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (h *ProxyQuerier) LabelNames(ctx context.Context, hints *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"took": time.Since(start),
		}).Debug("LabelNames")
	}()

	var matchersStrings []string
	if len(matchers) > 0 {
		s, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return nil, nil, err
		}
		matchersStrings = []string{s}
	}

	v, w, err := h.Client.LabelNames(ctx, matchersStrings, h.Start, h.End)
	return v, promhttputil.WarningsConvert(w), err
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }
