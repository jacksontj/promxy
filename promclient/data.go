package promclient

import (
	"github.com/jacksontj/promxy/promhttputil"
)

type DataResult struct {
	Status    promhttputil.Status    `json:"status"`
	Data      promhttputil.QueryData `json:"data"`
	ErrorType promhttputil.ErrorType `json:"errorType,omitempty"`
	Error     string                 `json:"error,omitempty"`
}

func (s *DataResult) Merge(o *DataResult) error {
	result, err := promhttputil.MergeValues(s.Data.Result, o.Data.Result)
	if err != nil {
		return err
	}
	s.Data.Result = result
	return nil
}
