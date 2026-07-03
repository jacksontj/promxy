package test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sort"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	commoncfg "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// TestRemoteReadFromPromxy is an end-to-end test for issue #356: a remote_read
// client querying promxy at /api/v1/read must get back the raw samples promxy
// fans out and collects from its downstream server_groups.
//
// The topology mirrors production: two independent Prometheus v1 APIs (one per
// server_group), promxy in front of them serving the standard Prometheus
// remote-read handler (remote.NewReadHandler, exactly what cmd/promxy wires up
// via the vendored web API) backed by ProxyStorage, and a client speaking the
// remote-read wire protocol against promxy.
//
// Each downstream holds the same series (test_metric{foo="bar"}) but with a
// distinct constant value; the two server_groups carry distinct az labels, so a
// correct fan-out returns two series — az="a" (all 7) and az="b" (all 42).
// Constant values make the assertion robust to how the raw range selector lands
// on sample boundaries.
//
// This exercises the SAMPLES response type explicitly. See
// TestRemoteReadFromPromxyStreamedChunks for the STREAMED_XOR_CHUNKS path that a
// stock Prometheus remote_read client negotiates by default.
func TestRemoteReadFromPromxy(t *testing.T) {
	readURL := startRemoteReadPromxy(t)

	// The five loaded samples sit at t=0,30s,...,120s. Reading [0,120s] with a
	// raw range selector captures all of them.
	mint := int64(0)
	maxt := int64(120000)

	matcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "test_metric")
	if err != nil {
		t.Fatalf("build matcher: %v", err)
	}

	query, err := remote.ToQuery(mint, maxt, []*labels.Matcher{matcher}, &storage.SelectHints{Start: mint, End: maxt})
	if err != nil {
		t.Fatalf("ToQuery: %v", err)
	}
	readReq := &prompb.ReadRequest{
		Queries: []*prompb.Query{query},
		// SAMPLES only: promxy does not implement ChunkQuerier.
		AcceptedResponseTypes: []prompb.ReadRequest_ResponseType{prompb.ReadRequest_SAMPLES},
	}

	result := doRemoteRead(t, readURL, readReq)

	// Expect exactly two series (one per server_group), distinguished by az.
	type seriesData struct {
		lbls labels.Labels
		vals []float64
	}
	got := make([]seriesData, 0, len(result.Timeseries))
	for _, ts := range result.Timeseries {
		b := labels.NewScratchBuilder(len(ts.Labels))
		for _, l := range ts.Labels {
			b.Add(l.Name, l.Value)
		}
		b.Sort()
		vals := make([]float64, 0, len(ts.Samples))
		for _, s := range ts.Samples {
			if math.IsNaN(s.Value) { // ignore any boundary staleness markers
				continue
			}
			vals = append(vals, s.Value)
		}
		got = append(got, seriesData{lbls: b.Labels(), vals: vals})
	}
	sort.Slice(got, func(i, j int) bool { return got[i].lbls.String() < got[j].lbls.String() })

	if len(got) != 2 {
		t.Fatalf("expected 2 series (one per server_group), got %d: %+v", len(got), got)
	}

	want := []struct {
		lbls labels.Labels
		val  float64
	}{
		{labels.FromStrings(labels.MetricName, "test_metric", "az", "a", "foo", "bar"), 7},
		{labels.FromStrings(labels.MetricName, "test_metric", "az", "b", "foo", "bar"), 42},
	}
	for i, w := range want {
		if !labels.Equal(got[i].lbls, w.lbls) {
			t.Errorf("series %d: labels = %s, want %s", i, got[i].lbls, w.lbls)
		}
		if len(got[i].vals) == 0 {
			t.Errorf("series %d (%s): no samples returned", i, got[i].lbls)
			continue
		}
		for _, v := range got[i].vals {
			if v != w.val {
				t.Errorf("series %d (%s): sample value = %v, want %v", i, got[i].lbls, v, w.val)
			}
		}
	}
}

// TestRemoteReadFromPromxyStreamedChunks is the regression test for the fix that
// closed the remaining half of issue #356: a stock Prometheus remote_read client
// negotiates STREAMED_XOR_CHUNKS first (remote.AcceptedResponseTypes), which used
// to hit ProxyStorage.ChunkQuerier and return a 500 ("not implemented"). With
// ProxyChunkQuerier in place, the same fan-out that the SAMPLES test asserts must
// now come back over the chunked-streaming path.
//
// It drives the high-level remote.Client (the exact code path Prometheus uses),
// so the chunked negotiation and decoding are real, not hand-rolled.
func TestRemoteReadFromPromxyStreamedChunks(t *testing.T) {
	readURL := startRemoteReadPromxy(t)

	u, err := url.Parse(readURL)
	if err != nil {
		t.Fatalf("parse read url: %v", err)
	}
	client, err := remote.NewReadClient("test", &remote.ClientConfig{
		URL:              &commoncfg.URL{URL: u},
		Timeout:          model.Duration(30 * time.Second),
		ChunkedReadLimit: 1e9,
	})
	if err != nil {
		t.Fatalf("NewReadClient: %v", err)
	}

	mint := int64(0)
	maxt := int64(120000)
	matcher, err := labels.NewMatcher(labels.MatchEqual, labels.MetricName, "test_metric")
	if err != nil {
		t.Fatalf("build matcher: %v", err)
	}
	query, err := remote.ToQuery(mint, maxt, []*labels.Matcher{matcher}, &storage.SelectHints{Start: mint, End: maxt})
	if err != nil {
		t.Fatalf("ToQuery: %v", err)
	}

	// client.Read uses remote.AcceptedResponseTypes (STREAMED_XOR_CHUNKS first),
	// so this is the chunked path a real Prometheus takes.
	ss, err := client.Read(context.Background(), query, true)
	if err != nil {
		t.Fatalf("remote read (chunked) failed: %v", err)
	}

	type seriesData struct {
		lbls labels.Labels
		vals []float64
	}
	var got []seriesData
	for ss.Next() {
		s := ss.At()
		var vals []float64
		it := s.Iterator(nil)
		for {
			vt := it.Next()
			if vt == chunkenc.ValNone {
				break
			}
			if vt == chunkenc.ValFloat {
				_, v := it.At()
				if !math.IsNaN(v) {
					vals = append(vals, v)
				}
			}
		}
		if err := it.Err(); err != nil {
			t.Fatalf("series iterator: %v", err)
		}
		got = append(got, seriesData{lbls: s.Labels(), vals: vals})
	}
	if err := ss.Err(); err != nil {
		t.Fatalf("series set: %v", err)
	}
	sort.Slice(got, func(i, j int) bool { return labels.Compare(got[i].lbls, got[j].lbls) < 0 })

	if len(got) != 2 {
		t.Fatalf("expected 2 series (one per server_group), got %d: %+v", len(got), got)
	}
	want := []struct {
		lbls labels.Labels
		val  float64
	}{
		{labels.FromStrings(labels.MetricName, "test_metric", "az", "a", "foo", "bar"), 7},
		{labels.FromStrings(labels.MetricName, "test_metric", "az", "b", "foo", "bar"), 42},
	}
	for i, w := range want {
		if !labels.Equal(got[i].lbls, w.lbls) {
			t.Errorf("series %d: labels = %s, want %s", i, got[i].lbls, w.lbls)
		}
		if len(got[i].vals) == 0 {
			t.Errorf("series %d (%s): no samples returned", i, got[i].lbls)
			continue
		}
		for _, v := range got[i].vals {
			if v != w.val {
				t.Errorf("series %d (%s): sample value = %v, want %v", i, got[i].lbls, v, w.val)
			}
		}
	}
}

// startRemoteReadPromxy stands up two downstream Prometheus v1 APIs (one per
// server_group, tagged az=a / az=b via rawDoublePSConfig) and a promxy instance
// in front of them serving the vendored Prometheus remote-read handler
// (remote.NewReadHandler backed by ProxyStorage — exactly what cmd/promxy wires
// up). It returns the promxy /api/v1/read URL; all resources are torn down via
// t.Cleanup.
//
// Each downstream holds test_metric{foo="bar"} with a distinct constant value (7
// for az=a, 42 for az=b) at t=0,30s,...,120s.
func startRemoteReadPromxy(t *testing.T) string {
	t.Helper()

	storeA := promqltest.LoadedStorage(t, "load 30s\n  test_metric{foo=\"bar\"} 7 7 7 7 7")
	t.Cleanup(func() { storeA.Close() })
	storeB := promqltest.LoadedStorage(t, "load 30s\n  test_metric{foo=\"bar\"} 42 42 42 42 42")
	t.Cleanup(func() { storeB.Close() })

	srvA, addrA, stopA := startAPIForTest(storeA)
	t.Cleanup(func() { srvA.Shutdown(context.Background()); <-stopA })
	srvB, addrB, stopB := startAPIForTest(storeB)
	t.Cleanup(func() { srvB.Shutdown(context.Background()); <-stopB })

	ps := getProxyStorage(fmt.Sprintf(rawDoublePSConfig, addrA, addrB))
	t.Cleanup(func() { ps.GetState().Cancel(nil) })

	readHandler := remote.NewReadHandler(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		nil, // registerer
		ps,  // SampleAndChunkQueryable
		func() config.Config { return config.DefaultConfig },
		50000000, // remoteReadSampleLimit
		1000,     // remoteReadConcurrencyLimit
		1048576,  // remoteReadMaxBytesInFrame
	)
	mux := http.NewServeMux()
	mux.Handle("/api/v1/read", readHandler)
	promxySrv := httptest.NewServer(mux)
	t.Cleanup(promxySrv.Close)

	return promxySrv.URL + "/api/v1/read"
}

// doRemoteRead issues a remote-read request against url and returns the first
// (and only) query result, failing the test on any protocol error.
func doRemoteRead(t *testing.T, url string, req *prompb.ReadRequest) *prompb.QueryResult {
	t.Helper()

	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatalf("marshal read request: %v", err)
	}
	compressed := snappy.Encode(nil, data)

	httpReq, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(compressed))
	if err != nil {
		t.Fatalf("build http request: %v", err)
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "snappy")
	httpReq.Header.Set("Accept-Encoding", "snappy")
	httpReq.Header.Set("X-Prometheus-Remote-Read-Version", "0.1.0")

	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("do remote read: %v", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("remote read returned %d: %s", resp.StatusCode, string(body))
	}

	uncompressed, err := snappy.Decode(nil, body)
	if err != nil {
		t.Fatalf("snappy decode response: %v", err)
	}
	var readResp prompb.ReadResponse
	if err := proto.Unmarshal(uncompressed, &readResp); err != nil {
		t.Fatalf("unmarshal read response: %v", err)
	}
	if len(readResp.Results) != 1 {
		t.Fatalf("expected 1 query result, got %d", len(readResp.Results))
	}
	return readResp.Results[0]
}
