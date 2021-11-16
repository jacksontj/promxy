package middleware

import (
	"context"
	"net/http"
)

type contextKey int

const headerKey contextKey = 0

func NewProxyHeaders(h http.Handler, headers []string) *ProxyHeaders {
	return &ProxyHeaders{
		h:       h,
		headers: headers,
	}
}

type ProxyHeaders struct {
	h       http.Handler
	headers []string
}

func (p *ProxyHeaders) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hdrs := make(map[string]string, len(p.headers))
	for _, header := range p.headers {
		if v := r.Header.Get(header); v != "" {
			hdrs[header] = v
		}
	}

	p.h.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), headerKey, hdrs)))
}

func GetHeaders(ctx context.Context) map[string]string {
	v := ctx.Value(headerKey)
	if v == nil {
		return nil
	}

	return v.(map[string]string)
}
