package promhttputil

import (
	"encoding/json"
	"fmt"

	"github.com/prometheus/common/model"
)

// TODO: have the api.go thing export these
type Status string

const (
	StatusSuccess Status = "success"
	StatusError          = "error"
)

type ErrorType string

const (
	ErrorNone     ErrorType = ""
	ErrorTimeout            = "timeout"
	ErrorCanceled           = "canceled"
	ErrorExec               = "execution"
	ErrorBadData            = "bad_data"
	ErrorInternal           = "internal"
)

// TODO: clean up

type QueryData struct {
	ResultType model.ValueType `json:"resultType"`
	Result     model.Value     `json:"result"`
}

func (r *QueryData) UnmarshalJSON(data []byte) error {
	type Raw struct {
		ResultType model.ValueType  `json:"resultType"`
		Result     *json.RawMessage `json:"result"`
	}
	rawData := &Raw{}
	if err := json.Unmarshal(data, rawData); err != nil {
		return err
	}
	r.ResultType = rawData.ResultType

	switch r.ResultType {
	case model.ValNone:
		return nil
	case model.ValScalar:
		tmp := &model.Scalar{}
		if err := json.Unmarshal(*rawData.Result, tmp); err == nil {
			r.Result = tmp
		} else {
			return err
		}
	case model.ValVector:
		tmp := model.Vector{}
		if err := json.Unmarshal(*rawData.Result, &tmp); err == nil {
			r.Result = tmp
		} else {
			return err
		}
	case model.ValMatrix:
		tmp := model.Matrix{}
		if err := json.Unmarshal(*rawData.Result, &tmp); err == nil {
			r.Result = tmp
		} else {
			return err
		}
	case model.ValString:
		tmp := &model.String{}
		if err := json.Unmarshal(*rawData.Result, tmp); err == nil {
			r.Result = tmp
		} else {
			return err
		}
	default:
		return fmt.Errorf("Unknown result type: %v", r.ResultType)
	}
	return nil
}
