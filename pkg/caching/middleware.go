package caching

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
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

func CachingMiddleware(next http.Handler, currentState func() []ServergroupState) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		useMiddleware := false
		paths := []string{
			"/api/v1/query",
			"/api/v1/query_range",
			"/api/v1/labels",
			"/api/v1/series",
		}
		for _, path := range paths {
			if r.URL.Path == path {
				useMiddleware = true
				break
			}
		}

		if !useMiddleware {
			if strings.HasPrefix(r.URL.Path, "/api/v1/label/") {
				useMiddleware = true
			}
		}

		if !useMiddleware {
			next.ServeHTTP(w, r)
			return
		}

		base := RequestContext{ServergroupsStates: currentState()}
		rCtx := NewRequestContext()

		// Set url
		base.URL = r.URL.RawPath
		rCtx.URL = r.URL.RawPath

		// Set Body
		if r.Body != nil {
			bodyBytes, _ := ioutil.ReadAll(r.Body)
			base.Body = bodyBytes
			rCtx.Body = bodyBytes
			r.Body = ioutil.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// Check for If-None-Match header
		if oldEtag := r.Header.Get("If-None-Match"); oldEtag != "" {
			// TODO: trim quotes around oldEtag if there are some?
			if oldEtag == base.GetEtag() {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}

		// Our middleware logic goes here...
		next.ServeHTTP(&ResponseWriter{ResponseWriter: w, rCtx: rCtx}, r.WithContext(WithRequestContext(r.Context(), rCtx)))
	})
}

type key string

const (
	request key = "cache_context"
)

type ServergroupState struct {
	l sync.Mutex
	// Hash of config state
	H uint64
	// FailedTargets is the list of targets unable to get a response from
	FailedTargets []string
}

type RequestContext struct {
	l sync.Mutex

	// ordinal -> hash
	URL                string
	Body               []byte
	ServergroupsStates []ServergroupState
}

func (r *RequestContext) SetServerHash(i int, v uint64) {
	r.l.Lock()
	defer r.l.Unlock()
	if len(r.ServergroupsStates) < i+1 {
		for x := 0; x <= i+1-len(r.ServergroupsStates); x++ {
			r.ServergroupsStates = append(r.ServergroupsStates, ServergroupState{})
		}
	}
	r.ServergroupsStates[i].H = v
}

func (r *RequestContext) AddFailedTarget(i int, t string) {
	fmt.Println("set failure", i, t)
	r.l.Lock()
	defer r.l.Unlock()
	if i >= len(r.ServergroupsStates) {
		panic("what")
	}
	r.ServergroupsStates[i].FailedTargets = append(r.ServergroupsStates[i].FailedTargets, t)
}

// TODO: this needs to include something about the query we are doing
func (r *RequestContext) GetEtag() string {
	r.l.Lock()
	defer r.l.Unlock()

	b, _ := json.Marshal(r)

	return fmt.Sprintf("%d-%x", len(b), sha1.Sum(b))
}

func NewRequestContext() *RequestContext {
	return &RequestContext{
		ServergroupsStates: make([]ServergroupState, 0, 10),
	}
}

func GetRequestContext(ctx context.Context) *RequestContext {
	if val, ok := ctx.Value(request).(*RequestContext); ok {
		return val
	}
	return nil
}

func WithRequestContext(ctx context.Context, rc *RequestContext) context.Context {
	return context.WithValue(ctx, request, rc)
}
