package promclient

import (
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
)

//easyjson:json
type LabelResult struct {
	Status    promhttputil.Status    `json:"status"`
	Data      []model.LabelValue     `json:"data"`
	ErrorType promhttputil.ErrorType `json:"errorType,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

func MergeLabelValues(a, b []model.LabelValue) []model.LabelValue {
	labels := make(map[model.LabelValue]struct{})
	for _, item := range a {
		labels[item] = struct{}{}
	}

	for _, item := range b {
		if _, ok := labels[item]; !ok {
			a = append(a, item)
			labels[item] = struct{}{}
		}
	}
	return a
}

func MergeLabelSets(a, b []model.LabelSet) []model.LabelSet {
	added := make(map[model.Fingerprint]struct{})
	for _, item := range a {
		added[item.Fingerprint()] = struct{}{}
	}

	for _, item := range b {
		fp := item.Fingerprint()
		if _, ok := added[fp]; !ok {
			added[fp] = struct{}{}
			a = append(a, item)
		}
	}

	return a
}
