package promclient

import (
	"fmt"

	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
)

type SeriesResult struct {
	Status    string                 `json:"status"`
	Data      []model.LabelSet       `json:"data"`
	ErrorType promhttputil.ErrorType `json:"errorType,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

func (s *SeriesResult) Merge(o *SeriesResult) error {
	if s.Status == "" {
		s.Status = o.Status
		s.Data = o.Data
		return nil
	}
	// TODO: need to know all the types and have logic -- pick the worst of the bunch
	if s.Status != o.Status {
		return fmt.Errorf("mismatch status")
	}

	sigs := make(map[model.Fingerprint]struct{})
	for _, res := range s.Data {
		sigs[res.Fingerprint()] = struct{}{}
	}

	// TODO; dedupe etc.
	for _, result := range o.Data {
		// calculate sig
		sig := result.Fingerprint()
		if _, ok := sigs[sig]; !ok {
			s.Data = append(s.Data, result)
			sigs[sig] = struct{}{}
		}
	}
	return nil
}
