package proxyquerier

import (
	"context"
	"time"

	"github.com/pkg/errors"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/storage"
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

// TODO: switch based on sortSeries bool(first arg)
// Select returns a set of series that matches the given label matchers.
func (h *ProxyQuerier) Select(_ bool, hints *storage.SelectHints, matchers ...*labels.Matcher) storage.SeriesSet {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"selectHints": hints,
			"matchers":    matchers,
			"took":        time.Since(start),
		}).Debug("Select")
	}()

	var result model.Value
	// TODO: get warnings from lower layers
	var warnings storage.Warnings
	var err error
	// Select() is a combined API call for query/query_range/series.
	// as of right now there is no great way of differentiating between a
	// data call (query/query_range) and a metadata call (series). For now
	// the working workaround is to switch based on the hints.
	// https://github.com/prometheus/prometheus/issues/4057
	if hints == nil {
		matcherString, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return NewSeriesSet(nil, nil, err)
		}
		labelsets, w, err := h.Client.Series(h.Ctx, []string{matcherString}, h.Start, h.End)
		warnings = promhttputil.WarningsConvert(w)
		if err != nil {
			return NewSeriesSet(nil, warnings, errors.Cause(err))
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
		result, w, err = h.Client.GetValue(h.Ctx, timestamp.Time(hints.Start), timestamp.Time(hints.End), matchers)
		warnings = promhttputil.WarningsConvert(w)
	}
	if err != nil {
		return NewSeriesSet(nil, warnings, errors.Cause(err))
	}

	iterators := promclient.IteratorsForValue(result)

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &Series{iterator}
	}

	return NewSeriesSet(series, warnings, nil)
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(name string) ([]string, storage.Warnings, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Since(start),
		}).Debug("LabelValues")
	}()

	result, w, err := h.Client.LabelValues(h.Ctx, name)
	warnings := promhttputil.WarningsConvert(w)
	if err != nil {
		return nil, warnings, errors.Cause(err)
	}

	ret := make([]string, len(result))
	for i, r := range result {
		ret[i] = string(r)
	}

	return ret, warnings, nil
}

// LabelNames returns all the unique label names present in the block in sorted order.
func (h *ProxyQuerier) LabelNames() ([]string, storage.Warnings, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"took": time.Since(start),
		}).Debug("LabelNames")
	}()

	v, w, err := h.Client.LabelNames(h.Ctx)
	return v, promhttputil.WarningsConvert(w), err
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }
