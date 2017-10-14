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

var CorsHeaders = map[string]string{
	"Access-Control-Allow-Headers":  "Accept, Authorization, Content-Type, Origin",
	"Access-Control-Allow-Methods":  "GET, OPTIONS",
	"Access-Control-Allow-Origin":   "*",
	"Access-Control-Expose-Headers": "Date",
}

// Enables cross-site script calls.
func SetCORS(w http.ResponseWriter) {
	for h, v := range CorsHeaders {
		w.Header().Set(h, v)
	}
}

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

type ApiError struct {
	Typ ErrorType
	Err error
}

func (e *ApiError) Error() string {
	return fmt.Sprintf("%s: %s", e.Typ, e.Err)
}

func RespondError(w http.ResponseWriter, apiErr *ApiError, data interface{}) {
	w.Header().Set("Content-Type", "application/json")

	var code int
	switch apiErr.Typ {
	case ErrorBadData:
		code = http.StatusBadRequest
	case ErrorExec:
		code = 422
	case ErrorCanceled, ErrorTimeout:
		code = http.StatusServiceUnavailable
	case ErrorInternal:
		code = http.StatusInternalServerError
	default:
		code = http.StatusInternalServerError
	}
	w.WriteHeader(code)

	b, err := json.Marshal(&Response{
		Status:    StatusError,
		ErrorType: apiErr.Typ,
		Error:     apiErr.Err.Error(),
		Data:      data,
	})
	if err != nil {
		return
	}
	w.Write(b)
}
