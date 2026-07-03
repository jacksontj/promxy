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
	"sort"
	"testing"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/prompb"
	"github.com/prometheus/prometheus/promql/promqltest"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/storage/remote"
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
// Note: promxy's ProxyStorage does not implement ChunkQuerier, so it only serves
// the SAMPLES response type (not STREAMED_XOR_CHUNKS). The request below asks for
// SAMPLES explicitly, which is the path that works today; a chunked-streaming
// client is unsupported.
func TestRemoteReadFromPromxy(t *testing.T) {
	// Two downstream Prometheus instances, one per server_group, each loaded
	// with the same series name but a distinct constant value.
	loadA := `load 30s
  test_metric{foo="bar"} 7 7 7 7 7`
	loadB := `load 30s
  test_metric{foo="bar"} 42 42 42 42 42`

	storeA := promqltest.LoadedStorage(t, loadA)
	defer storeA.Close()
	storeB := promqltest.LoadedStorage(t, loadB)
	defer storeB.Close()

	srvA, addrA, stopA := startAPIForTest(storeA)
	defer func() {
		srvA.Shutdown(context.Background())
		<-stopA
	}()
	srvB, addrB, stopB := startAPIForTest(storeB)
	defer func() {
		srvB.Shutdown(context.Background())
		<-stopB
	}()

	// promxy in front of both downstreams; rawDoublePSConfig tags the groups
	// with az=a / az=b.
	ps := getProxyStorage(fmt.Sprintf(rawDoublePSConfig, addrA, addrB))
	defer ps.GetState().Cancel(nil)

	// Serve promxy's remote-read endpoint the same way cmd/promxy does: the
	// vendored Prometheus read handler backed by the proxy storage.
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
	defer promxySrv.Close()

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

	result := doRemoteRead(t, promxySrv.URL+"/api/v1/read", readReq)

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
