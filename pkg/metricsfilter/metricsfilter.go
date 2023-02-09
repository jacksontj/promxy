package metricsfilter

import (
	"github.com/emirpasic/gods/sets/hashset"
	"github.com/prometheus/common/model"
)

type MetricsAllowed struct {
	items hashset.Set
	count int
}

func NewMetricAllowed() *MetricsAllowed {
	m := MetricsAllowed{items: *hashset.New(), count: 0}
	return &m
}

func (m *MetricsAllowed) Update(items *model.LabelValues) *MetricsAllowed {

	m.items.Clear()
	for _, item := range *items {
		m.items.Add(string(item))
	}
	return m
}

func (m *MetricsAllowed) Contains(metricName string) bool {
	return m.items.Contains(metricName)
}
