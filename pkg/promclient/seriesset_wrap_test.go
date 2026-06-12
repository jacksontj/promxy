package promclient

import (
	"errors"
	"testing"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/storage"
	"github.com/prometheus/prometheus/util/annotations"

	"github.com/jacksontj/promxy/pkg/promapi"
)

func TestMapErrSeriesSet(t *testing.T) {
	boom := errors.New("boom")

	// Clearing an error.
	got := MapErrSeriesSet(storage.ErrSeriesSet(boom), func(error) error { return nil })
	if got.Err() != nil {
		t.Fatalf("expected error cleared, got %v", got.Err())
	}

	// Transforming an error.
	wrapped := MapErrSeriesSet(storage.ErrSeriesSet(boom), func(e error) error {
		return errors.New("wrapped: " + e.Error())
	})
	if wrapped.Err() == nil || wrapped.Err().Error() != "wrapped: boom" {
		t.Fatalf("expected wrapped error, got %v", wrapped.Err())
	}

	// fn receives nil when there is no error and may introduce one.
	introduced := MapErrSeriesSet(promapi.NewSeriesSet(nil, nil, nil), func(e error) error {
		if e != nil {
			t.Fatalf("expected nil error passed to fn, got %v", e)
		}
		return boom
	})
	if !errors.Is(introduced.Err(), boom) {
		t.Fatalf("expected introduced error, got %v", introduced.Err())
	}
}

func TestDowngradeErrSeriesSet(t *testing.T) {
	boom := errors.New("downstream failed")
	ss := DowngradeErrSeriesSet(storage.ErrSeriesSet(boom))

	if ss.Err() != nil {
		t.Fatalf("expected Err() demoted to nil, got %v", ss.Err())
	}
	found := false
	for _, e := range ss.Warnings().AsErrors() {
		if errors.Is(e, boom) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected the error to appear in Warnings(), got %v", ss.Warnings().AsErrors())
	}
}

func TestWithWarnings(t *testing.T) {
	w := annotations.New().Add(errors.New("a warning"))
	ss := WithWarnings(promapi.NewSeriesSet(nil, nil, nil), w)
	if len(ss.Warnings().AsErrors()) != 1 {
		t.Fatalf("expected the overridden warnings, got %v", ss.Warnings().AsErrors())
	}
}

func TestMergeAnnotations(t *testing.T) {
	a := annotations.New().Add(errors.New("a"))
	b := annotations.New().Add(errors.New("b"))
	got := MergeAnnotations(a, b)
	if len(got) != 2 {
		t.Fatalf("expected union of 2, got %d: %v", len(got), got.AsErrors())
	}

	// a may be nil.
	got = MergeAnnotations(nil, b)
	if len(got) != 1 {
		t.Fatalf("expected 1 from nil base, got %d", len(got))
	}
}

func TestMapLabelsSeriesSet(t *testing.T) {
	in := promapi.NewSeriesSet([]storage.Series{
		storage.NewListSeries(labels.FromStrings("__name__", "up", "job", "a"), nil),
	}, nil, nil)

	got := MapLabelsSeriesSet(in, func(l labels.Labels) labels.Labels {
		return labels.NewBuilder(l).Set("extra", "1").Labels()
	})

	if !got.Next() {
		t.Fatal("expected one series")
	}
	if v := got.At().Labels().Get("extra"); v != "1" {
		t.Fatalf("expected rewritten label extra=1, got %q", v)
	}
}
