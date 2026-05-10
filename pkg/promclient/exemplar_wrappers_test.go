package promclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/relabel"
)

// recordingExemplarAPI is a minimal stub that records the calls it receives
// and returns a canned response. Embeds stubAPI so it picks up no-op
// implementations of the rest of the API surface.
type recordingExemplarAPI struct {
	stubAPI
	calls []recordedCall
	resp  []v1.ExemplarQueryResult
}

type recordedCall struct {
	query string
	start time.Time
	end   time.Time
}

func (r *recordingExemplarAPI) QueryExemplars(_ context.Context, query string, start, end time.Time) ([]v1.ExemplarQueryResult, error) {
	r.calls = append(r.calls, recordedCall{query, start, end})
	return r.resp, nil
}

func TestAddLabelClient_QueryExemplarsTagsSeriesLabels(t *testing.T) {
	stub := &recordingExemplarAPI{
		resp: []v1.ExemplarQueryResult{
			{
				SeriesLabels: model.LabelSet{"__name__": "foo"},
				Exemplars:    []v1.Exemplar{{Value: 1, Timestamp: 100}},
			},
		},
	}
	c := &AddLabelClient{API: stub, Labels: model.LabelSet{"az": "a"}}

	got, err := c.QueryExemplars(context.Background(), `foo`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 result, got %d", len(got))
	}
	if got[0].SeriesLabels["az"] != "a" {
		t.Fatalf("expected az=a in SeriesLabels, got %v", got[0].SeriesLabels)
	}
	if got[0].SeriesLabels["__name__"] != "foo" {
		t.Fatalf("expected original __name__ preserved, got %v", got[0].SeriesLabels)
	}
}

func TestMetricsRelabelClient_QueryExemplarsRelabelsAndDrops(t *testing.T) {
	stub := &recordingExemplarAPI{
		resp: []v1.ExemplarQueryResult{
			{SeriesLabels: model.LabelSet{"__name__": "keep_me"}},
			{SeriesLabels: model.LabelSet{"__name__": "drop_me"}},
		},
	}

	dropRule, err := relabel.NewRegexp("drop_me")
	if err != nil {
		t.Fatal(err)
	}
	c := &MetricsRelabelClient{
		API: stub,
		RelabelConfigs: []*relabel.Config{
			{
				SourceLabels: model.LabelNames{model.MetricNameLabel},
				Action:       relabel.Drop,
				Regex:        dropRule,
			},
		},
	}

	got, err := c.QueryExemplars(context.Background(), `{__name__=~"keep_me|drop_me"}`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 series after drop, got %d (%+v)", len(got), got)
	}
	if got[0].SeriesLabels["__name__"] != "keep_me" {
		t.Fatalf("kept the wrong series: %v", got[0].SeriesLabels)
	}
}

func TestAbsoluteTimeFilter_QueryExemplars(t *testing.T) {
	tests := []struct {
		name        string
		filterStart time.Time
		filterEnd   time.Time
		truncate    bool
		queryStart  time.Time
		queryEnd    time.Time
		wantCalled  bool
		// truncate-mode: the request times we expect to forward
		expectStart time.Time
		expectEnd   time.Time
	}{
		{
			name:        "in window passes through unchanged",
			filterStart: time.Unix(100, 0),
			filterEnd:   time.Unix(200, 0),
			queryStart:  time.Unix(120, 0),
			queryEnd:    time.Unix(180, 0),
			wantCalled:  true,
			expectStart: time.Unix(120, 0),
			expectEnd:   time.Unix(180, 0),
		},
		{
			name:        "entirely after window short-circuits",
			filterStart: time.Unix(100, 0),
			filterEnd:   time.Unix(200, 0),
			queryStart:  time.Unix(300, 0),
			queryEnd:    time.Unix(400, 0),
			wantCalled:  false,
		},
		{
			name:        "entirely before window short-circuits",
			filterStart: time.Unix(100, 0),
			filterEnd:   time.Unix(200, 0),
			queryStart:  time.Unix(10, 0),
			queryEnd:    time.Unix(50, 0),
			wantCalled:  false,
		},
		{
			name:        "truncate clamps to filter window",
			filterStart: time.Unix(100, 0),
			filterEnd:   time.Unix(200, 0),
			truncate:    true,
			queryStart:  time.Unix(50, 0),
			queryEnd:    time.Unix(300, 0),
			wantCalled:  true,
			expectStart: time.Unix(100, 0),
			expectEnd:   time.Unix(200, 0),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stub := &recordingExemplarAPI{}
			f := &AbsoluteTimeFilter{API: stub, Start: tc.filterStart, End: tc.filterEnd, Truncate: tc.truncate}
			_, err := f.QueryExemplars(context.Background(), `foo`, tc.queryStart, tc.queryEnd)
			if err != nil {
				t.Fatalf("QueryExemplars: %v", err)
			}
			if tc.wantCalled {
				if len(stub.calls) != 1 {
					t.Fatalf("want 1 downstream call, got %d", len(stub.calls))
				}
				if !stub.calls[0].start.Equal(tc.expectStart) || !stub.calls[0].end.Equal(tc.expectEnd) {
					t.Fatalf("call window: want %v..%v got %v..%v",
						tc.expectStart, tc.expectEnd, stub.calls[0].start, stub.calls[0].end)
				}
			} else if len(stub.calls) != 0 {
				t.Fatalf("expected no downstream call, got %d (%+v)", len(stub.calls), stub.calls)
			}
		})
	}
}

func TestAddLabelClient_QueryExemplarsShortCircuits(t *testing.T) {
	stub := &recordingExemplarAPI{
		resp: []v1.ExemplarQueryResult{
			{SeriesLabels: model.LabelSet{"__name__": "foo"}},
		},
	}
	c := &AddLabelClient{API: stub, Labels: model.LabelSet{"az": "a"}}

	// Query asks for az="b" — incompatible with our az="a", must short-circuit.
	got, err := c.QueryExemplars(context.Background(), `foo{az="b"}`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil result for incompatible az, got %v", got)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("expected no downstream call, got %d", len(stub.calls))
	}

	// Query asks for az="a" — must forward.
	stub.calls = nil
	got, err = c.QueryExemplars(context.Background(), `foo{az="a"}`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 downstream call, got %d", len(stub.calls))
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
}

func TestLabelFilterClient_QueryExemplarsShortCircuits(t *testing.T) {
	stub := &recordingExemplarAPI{
		resp: []v1.ExemplarQueryResult{
			{SeriesLabels: model.LabelSet{"__name__": "foo"}},
		},
	}
	// Filter says we only know about metric_a — anything else short-circuits.
	c := &LabelFilterClient{API: stub}
	c.filter.Store(map[string]map[string]struct{}{
		"__name__": {"metric_a": {}},
	})

	got, err := c.QueryExemplars(context.Background(), `metric_b`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for filtered-out metric, got %v", got)
	}
	if len(stub.calls) != 0 {
		t.Fatalf("expected no downstream call, got %d", len(stub.calls))
	}

	stub.calls = nil
	_, err = c.QueryExemplars(context.Background(), `metric_a`, time.Unix(0, 0), time.Unix(1, 0))
	if err != nil {
		t.Fatalf("QueryExemplars: %v", err)
	}
	if len(stub.calls) != 1 {
		t.Fatalf("expected 1 downstream call, got %d", len(stub.calls))
	}
}

func TestErrorWrap_QueryExemplarsWrapsError(t *testing.T) {
	stub := &errorAPI{API: &stubAPI{}, err: fmt.Errorf("inner")}
	w := &ErrorWrap{A: stub, Msg: "outer"}
	_, err := w.QueryExemplars(context.Background(), `foo`, time.Unix(0, 0), time.Unix(1, 0))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); got != "outer: inner" {
		t.Fatalf("error message: want %q got %q", "outer: inner", got)
	}
}
