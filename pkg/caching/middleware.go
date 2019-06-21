package caching

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/http"
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
		base := RequestContext{servergroupsStates: currentState()}

		rCtx := NewRequestContext()

		fmt.Println("middleware inline!")

		// Check for If-None-Match header
		if oldEtag := r.Header.Get("If-None-Match"); oldEtag != "" {
			// TODO: trim quotes around oldEtag if there are some?
			if oldEtag == base.GetEtag() {
				w.WriteHeader(http.StatusNotModified)
				return
			}
		}
		fmt.Println("doing handler")

		// Our middleware logic goes here...
		next.ServeHTTP(&ResponseWriter{ResponseWriter: w, rCtx: rCtx}, r.WithContext(WithRequestContext(r.Context(), rCtx)))

		fmt.Println(rCtx)
		fmt.Println(rCtx.GetEtag())
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
	servergroupsStates []ServergroupState
}

func (r *RequestContext) SetServerHash(i int, v uint64) {
	r.l.Lock()
	defer r.l.Unlock()
	if len(r.servergroupsStates) < i+1 {
		for x := 0; x <= i+1-len(r.servergroupsStates); x++ {
			r.servergroupsStates = append(r.servergroupsStates, ServergroupState{})
		}
	}
	r.servergroupsStates[i].H = v
}

func (r *RequestContext) AddFailedTarget(i int, t string) {
	fmt.Println("set failure", i, t)
	r.l.Lock()
	defer r.l.Unlock()
	if i >= len(r.servergroupsStates) {
		panic("what")
	}
	r.servergroupsStates[i].FailedTargets = append(r.servergroupsStates[i].FailedTargets, t)
}

func (r *RequestContext) GetEtag() string {
	r.l.Lock()
	defer r.l.Unlock()

	b, _ := json.Marshal(r.servergroupsStates)

	return fmt.Sprintf("%d-%x", len(b), sha1.Sum(b))
}

func NewRequestContext() *RequestContext {
	return &RequestContext{
		servergroupsStates: make([]ServergroupState, 0, 10),
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
