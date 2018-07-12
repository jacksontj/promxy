package promclient

import (
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/prometheus/common/model"
)

//easyjson:json
type SeriesResult struct {
	Status    promhttputil.Status    `json:"status"`
	Data      []model.LabelSet       `json:"data"`
	ErrorType promhttputil.ErrorType `json:"errorType,omitempty"`
	Error     string                 `json:"error,omitempty"`
}
