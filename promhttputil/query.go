package promhttputil

import (
	"encoding/json"
	"fmt"
	"net/http"

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

type Response struct {
	Status    Status      `json:"status"`
	Data      interface{} `json:"data,omitempty"`
	ErrorType ErrorType   `json:"errorType,omitempty"`
	Error     string      `json:"error,omitempty"`
}

func (r *Response) UnmarshalJSON(data []byte) error {
	type Raw struct {
		Status    Status           `json:"status"`
		Data      *json.RawMessage `json:"data,omitempty"`
		ErrorType ErrorType        `json:"errorType,omitempty"`
		Error     string           `json:"error,omitempty"`
	}
	rawData := &Raw{}
	if err := json.Unmarshal(data, rawData); err != nil {
		return err
	}
	r.Status = rawData.Status
	r.ErrorType = rawData.ErrorType
	r.Error = rawData.Error

	// Lets try to unpack
	qd := &QueryData{}
	if err := json.Unmarshal(*rawData.Data, qd); err == nil {
		r.Data = qd
		return nil
	} else {
		fmt.Println("Unable to marshal", err)
		return json.Unmarshal(*rawData.Data, r.Data)
	}
}

func Respond(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	b, err := json.Marshal(&Response{
		Status: StatusSuccess,
		Data:   data,
	})
	if err != nil {
		return
	}
	w.Write(b)
}
