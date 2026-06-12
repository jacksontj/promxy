package proxyquerier

import (
	"context"
	"time"

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

	// Select() is a combined API call for query/query_range/series.
	// as of right now there is no great way of differentiating between a
	// data call (query/query_range) and a metadata call (series). For now
	// the working workaround is to switch based on the hints.
	// https://github.com/prometheus/prometheus/issues/4057
	if hints == nil || hints.Func == "series" {
		matcherString, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return storage.ErrSeriesSet(err)
		}
		labelsets, w, err := h.Client.Series(ctx, []string{matcherString}, h.Start, h.End)
		warnings := promhttputil.WarningsConvert(w)
		if err != nil {
			return NewSeriesSet(nil, warnings, err)
		}
		// series metadata: label sets with no samples
		series := make([]storage.Series, len(labelsets))
		for j, labelset := range labelsets {
			lb := labels.NewScratchBuilder(len(labelset))
			for k, v := range labelset {
				lb.Add(string(k), string(v))
			}
			lb.Sort()
			series[j] = storage.NewListSeries(lb.Labels(), nil)
		}
		return NewSeriesSet(series, warnings, nil)
	}

	// Data path: the client already returns a storage.SeriesSet, decoded
	// straight from the downstream response (no model.Value round-trip).
	return h.Client.GetValue(ctx, timestamp.Time(hints.Start), timestamp.Time(hints.End), matchers)
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(ctx context.Context, name string, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
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
func (h *ProxyQuerier) LabelNames(ctx context.Context, _ *storage.LabelHints, matchers ...*labels.Matcher) ([]string, annotations.Annotations, error) {
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
