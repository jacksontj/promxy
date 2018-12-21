package proxyquerier

import (
	"context"
	"time"

	"github.com/jacksontj/promxy/config"
	"github.com/jacksontj/promxy/promclient"
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/jacksontj/promxy/servergroup"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/timestamp"
	"github.com/prometheus/prometheus/storage"
	"github.com/sirupsen/logrus"
)

type ProxyQuerier struct {
	Ctx          context.Context
	Start        time.Time
	End          time.Time
	ServerGroups servergroup.ServerGroups

	Cfg *proxyconfig.PromxyConfig
}

// Select returns a set of series that matches the given label matchers.
func (h *ProxyQuerier) Select(selectParams *storage.SelectParams, matchers ...*labels.Matcher) (storage.SeriesSet, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"selectParams": selectParams,
			"matchers":     matchers,
			"took":         time.Now().Sub(start),
		}).Debug("Select")
	}()

	var result model.Value
	var err error
	// Select() is a combined API call for query/query_range/series.
	// as of right now there is no great way of differentiating between a
	// data call (query/query_range) and a metadata call (series). For now
	// the working workaround is to switch based on the selectParams.
	// https://github.com/prometheus/prometheus/issues/4057
	if selectParams == nil {
		matcherString, err := promhttputil.MatcherToString(matchers)
		if err != nil {
			return nil, err
		}
		labelsets, err := h.ServerGroups.Series(h.Ctx, []string{matcherString}, h.Start, h.End)
		if err != nil {
			return nil, errors.Cause(err)
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
		result, err = h.ServerGroups.GetValue(h.Ctx, timestamp.Time(selectParams.Start), timestamp.Time(selectParams.End), matchers)
	}
	if err != nil {
		return nil, errors.Cause(err)
	}

	iterators := promclient.IteratorsForValue(result)

	series := make([]storage.Series, len(iterators))
	for i, iterator := range iterators {
		series[i] = &Series{iterator}
	}

	return NewSeriesSet(series), nil
}

// LabelValues returns all potential values for a label name.
func (h *ProxyQuerier) LabelValues(name string) ([]string, error) {
	start := time.Now()
	defer func() {
		logrus.WithFields(logrus.Fields{
			"name": name,
			"took": time.Now().Sub(start),
		}).Debug("LabelValues")
	}()

	result, err := h.ServerGroups.LabelValues(h.Ctx, name)
	if err != nil {
		return nil, errors.Cause(err)
	}

	ret := make([]string, len(result))
	for i, r := range result {
		ret[i] = string(r)
	}

	return ret, nil
}

// Close closes the querier. Behavior for subsequent calls to Querier methods
// is undefined.
func (h *ProxyQuerier) Close() error { return nil }
