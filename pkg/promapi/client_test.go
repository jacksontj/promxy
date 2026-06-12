package promapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
)

func TestClientQueryRoundTrip(t *testing.T) {
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotPath = r.URL.Path
		gotQuery = r.Form.Get("query")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"vector","result":[` +
			`{"metric":{"__name__":"up","job":"a"},"value":[100.000,"1"]},` +
			`{"metric":{"__name__":"up","job":"b"},"value":[100.000,"0"]}` +
			`]},"warnings":["PromQL warning: heads up"]}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	c := &Client{URL: u}

	ss := c.Query(context.Background(), "up", time.Unix(100, 0))
	got := dumpSeriesSet(t, ss)
	want := map[string]string{
		`{__name__="up", job="a"}`: "100000=1 ",
		`{__name__="up", job="b"}`: "100000=0 ",
	}
	if len(got) != len(want) {
		t.Fatalf("series: got %v", got)
	}
	for k, w := range want {
		if got[k] != w {
			t.Fatalf("series %s: got %q want %q", k, got[k], w)
		}
	}
	if gotPath != "/api/v1/query" || gotQuery != "up" {
		t.Fatalf("request: path=%q query=%q", gotPath, gotQuery)
	}
	// warnings should surface
	if w := ss.Warnings(); len(w) == 0 {
		t.Fatal("expected warnings to surface from the response")
	}
}

func TestClientSelectBuildsRangeSelector(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotQuery = r.Form.Get("query")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	u, _ := url.Parse(srv.URL)
	c := &Client{URL: u}
	m := labels.MustNewMatcher(labels.MatchEqual, "__name__", "up")
	_ = c.Select(context.Background(), time.Unix(0, 0), time.Unix(60, 0), m)

	if want := `{__name__="up"}[61s]`; gotQuery != want {
		t.Fatalf("select query: got %q want %q", gotQuery, want)
	}
}
