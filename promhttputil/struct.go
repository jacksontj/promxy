package promhttputil

import (
	"fmt"

	"github.com/mailru/easyjson/jlexer"
	"github.com/prometheus/common/model"
)

// TODO: have the api.go thing export these
// attempted once already -- https://github.com/prometheus/prometheus/pull/3615
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

// UnmarshalJSON supports json.Unmarshaler interface
func (v *QueryData) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	v.UnmarshalEasyJSON(&r)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (out *QueryData) UnmarshalEasyJSON(in *jlexer.Lexer) {
	isTopLevel := in.IsStart()
	if in.IsNull() {
		if isTopLevel {
			in.Consumed()
		}
		in.Skip()
		return
	}
	in.Delim('{')
	for !in.IsDelim('}') {
		key := in.UnsafeString()
		in.WantColon()
		if in.IsNull() {
			in.Skip()
			in.WantComma()
			continue
		}
		switch key {
		case "resultType":
			out.ResultType.UnmarshalJSON(in.Raw())
		case "result":
			switch out.ResultType {
			case model.ValNone:
				return
			case model.ValScalar:
				tmp := &model.Scalar{}
				tmp.UnmarshalJSON(in.Raw())
				out.Result = tmp
			case model.ValVector:
				tmp := model.Vector{}
				tmp.UnmarshalEasyJSON(in)
				out.Result = tmp
			case model.ValMatrix:
				tmp := model.Matrix{}
				tmp.UnmarshalEasyJSON(in)
				out.Result = tmp
			case model.ValString:
				tmp := &model.String{}
				tmp.UnmarshalJSON(in.Raw())
				out.Result = tmp
			default:
				in.AddError(fmt.Errorf("Unknown result type: %v", out.ResultType))
			}
		default:
			in.SkipRecursive()
		}
		in.WantComma()
	}
	in.Delim('}')
	if isTopLevel {
		in.Consumed()
	}
}
