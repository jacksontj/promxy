package promapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func TestClientMetadataMethods(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/labels":
			w.Write([]byte(`{"status":"success","data":["__name__","job"]}`))
		case "/api/v1/label/job/values":
			w.Write([]byte(`{"status":"success","data":["a","b"]}`))
		case "/api/v1/series":
			w.Write([]byte(`{"status":"success","data":[{"__name__":"up","job":"a"},{"__name__":"up","job":"b"}]}`))
		case "/api/v1/metadata":
			w.Write([]byte(`{"status":"success","data":{"up":[{"type":"gauge","help":"h","unit":""}]}}`))
		}
	}))
	defer srv.Close()
	u, _ := url.Parse(srv.URL)
	c := &Client{URL: u}
	ctx := context.Background()
	z := time.Time{}

	if names, _, err := c.LabelNames(ctx, nil, z, z); err != nil || len(names) != 2 || names[0] != "__name__" {
		t.Fatalf("LabelNames: %v %v", names, err)
	}
	if vals, _, err := c.LabelValues(ctx, "job", nil, z, z); err != nil || len(vals) != 2 || vals[1] != "b" {
		t.Fatalf("LabelValues: %v %v", vals, err)
	}
	if s, _, err := c.Series(ctx, nil, z, z); err != nil || len(s) != 2 || s[0].Get("job") != "a" {
		t.Fatalf("Series: %v %v", s, err)
	}
	if md, err := c.Metadata(ctx, "", ""); err != nil || md["up"][0].Type != "gauge" {
		t.Fatalf("Metadata: %v %v", md, err)
	}
}
