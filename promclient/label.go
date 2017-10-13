package promclient

import (
	"fmt"

	"github.com/prometheus/common/model"
)

type LabelResult struct {
	Status string             `json:"status"`
	Data   []model.LabelValue `json:"data"`
}

func (l *LabelResult) Merge(o *LabelResult) error {
	if l.Status == "" {
		l.Status = o.Status
		l.Data = o.Data
		return nil
	}
	// TODO: need to know all the types and have logic -- pick the worst of the bunch
	if l.Status != o.Status {
		return fmt.Errorf("mismatch status")
	}

	labels := make(map[model.LabelValue]struct{})
	for _, item := range l.Data {
		labels[item] = struct{}{}
	}

	for _, item := range o.Data {
		if _, ok := labels[item]; !ok {
			l.Data = append(l.Data, item)
			labels[item] = struct{}{}
		}
	}
	return nil
}
