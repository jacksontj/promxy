package promclient

import (
	"github.com/jacksontj/promxy/promhttputil"
	"github.com/mailru/easyjson/jlexer"
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

// UnmarshalJSON supports json.Unmarshaler interface
func (v *DataResult) UnmarshalJSON(data []byte) error {
	r := jlexer.Lexer{Data: data}
	v.UnmarshalEasyJSON(&r)
	return r.Error()
}

// UnmarshalEasyJSON supports easyjson.Unmarshaler interface
func (out *DataResult) UnmarshalEasyJSON(in *jlexer.Lexer) {
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
		case "status":
			out.Status = promhttputil.Status(in.String())
		case "data":
			in.AddError(out.Data.UnmarshalJSON(in.Raw()))
		case "errorType":
			out.ErrorType = promhttputil.ErrorType(in.String())
		case "error":
			out.Error = string(in.String())
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
