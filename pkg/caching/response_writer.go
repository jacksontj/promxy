package caching

import (
	"net/http"
)

type ResponseWriter struct {
	http.ResponseWriter
	rCtx *RequestContext
}

func (r *ResponseWriter) WriteHeader(statusCode int) {
	if statusCode == 200 {
		r.ResponseWriter.Header().Set("Etag", r.rCtx.GetEtag())
		// TODO: set Cache-Control headers
	}
	r.ResponseWriter.WriteHeader(statusCode)
}
