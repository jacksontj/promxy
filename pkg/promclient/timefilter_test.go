package promclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
)

type timeFilterTestCase struct {
	validTimes   []time.Time
	invalidTimes []time.Time

	validRanges   []v1.Range
	invalidRanges []v1.Range
}

func timefilterTest(t *testing.T, api API, testCase timeFilterTestCase) {
	for i, validTime := range testCase.validTimes {
		t.Run(fmt.Sprintf("validTime_%d", i), func(t *testing.T) {
			t.Run("query", func(t *testing.T) {
				if _, _, err := api.Query(context.TODO(), "", validTime); err == nil {
					t.Fatalf("Missing call to API")
				}
			})
		})
	}
	for i, invalidTime := range testCase.invalidTimes {
		t.Run(fmt.Sprintf("invalidTime_%d", i), func(t *testing.T) {
			t.Run("query", func(t *testing.T) {
				if _, _, err := api.Query(context.TODO(), "", invalidTime); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})
		})
	}

	for i, r := range testCase.validRanges {
		t.Run(fmt.Sprintf("validRange_%d", i), func(t *testing.T) {
			t.Run("label_names", func(t *testing.T) {
				if _, _, err := api.LabelNames(context.TODO(), []string{"a"}, r.Start, r.End); err == nil {
					t.Fatalf("Missing call to API")
				}
			})

			t.Run("label_values", func(t *testing.T) {
				if _, _, err := api.LabelValues(context.TODO(), "__name__", []string{"a"}, r.Start, r.End); err == nil {
					t.Fatalf("Missing call to API")
				}
			})

			t.Run("query_range", func(t *testing.T) {
				if _, _, err := api.QueryRange(context.TODO(), "", v1.Range{Start: r.Start, End: r.End}); err == nil {
					t.Fatalf("Missing call to API")
				}
			})
			t.Run("series", func(t *testing.T) {
				if _, _, err := api.Series(context.TODO(), nil, r.Start, r.End); err == nil {
					t.Fatalf("Missing call to API")
				}
			})
			t.Run("getvalue", func(t *testing.T) {
				if _, _, err := api.GetValue(context.TODO(), r.Start, r.End, nil); err == nil {
					t.Fatalf("Missing call to API")
				}
			})
		})
	}
	for i, r := range testCase.invalidRanges {
		t.Run(fmt.Sprintf("invalidRange_%d", i), func(t *testing.T) {
			t.Run("label_names", func(t *testing.T) {
				if _, _, err := api.LabelNames(context.TODO(), []string{"a"}, r.Start, r.End); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})

			t.Run("label_values", func(t *testing.T) {
				if _, _, err := api.LabelValues(context.TODO(), "__name__", []string{"a"}, r.Start, r.End); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})

			t.Run("query_range", func(t *testing.T) {
				if _, _, err := api.QueryRange(context.TODO(), "", v1.Range{Start: r.Start, End: r.End}); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})
			t.Run("series", func(t *testing.T) {
				if _, _, err := api.Series(context.TODO(), nil, r.Start, r.End); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})
			t.Run("getvalue", func(t *testing.T) {
				if _, _, err := api.GetValue(context.TODO(), r.Start, r.End, nil); err != nil {
					t.Fatalf("Unexpected call to API")
				}
			})
		})
	}
}

func TestAbsoluteTimeFilter(t *testing.T) {
	now := time.Now()

	start := now.Add(time.Hour * -2)
	end := now.Add(time.Hour * -1)

	t.Run(fmt.Sprintf("start=%s end=%s", start, end), func(t *testing.T) {
		api := &AbsoluteTimeFilter{
			API:   &recoverAPI{nil},
			Start: start,
			End:   end,
		}
		timefilterTest(t, api, timeFilterTestCase{
			validTimes: []time.Time{
				start,
				end,
				start.Add(time.Minute),
			},
			invalidTimes: []time.Time{
				now,
				start.Add(time.Minute * -1),
			},
			validRanges: []v1.Range{
				{Start: start, End: end},
				{Start: start.Add(time.Hour * -1), End: end},
				{Start: start, End: end.Add(time.Hour)},
			},
			invalidRanges: []v1.Range{
				{Start: now, End: now},
				{Start: start.Add(time.Hour * -10), End: end.Add(time.Hour * -9)},
			},
		})
	})

	end = time.Time{}

	t.Run(fmt.Sprintf("start=%s end=%s", start, end), func(t *testing.T) {
		api := &AbsoluteTimeFilter{
			API:   &recoverAPI{nil},
			Start: start,
			End:   end,
		}
		timefilterTest(t, api, timeFilterTestCase{
			validTimes: []time.Time{
				start,
				start.Add(time.Minute),
				now,
			},
			invalidTimes: []time.Time{
				start.Add(time.Minute * -1),
			},
			validRanges: []v1.Range{
				{Start: start, End: now},
				{Start: start.Add(time.Hour * -1), End: now},
				{Start: start, End: now.Add(time.Hour)},
				{Start: now, End: now},
			},
			invalidRanges: []v1.Range{
				{Start: start.Add(time.Hour * -10), End: now.Add(time.Hour * -9)},
			},
		})
	})
}

func TestRelativeTimeFilter(t *testing.T) {
	now := time.Now()

	startOffset := time.Hour * -2
	endOffset := time.Hour * -1

	start := now.Add(startOffset)
	end := now.Add(endOffset)

	api := &RelativeTimeFilter{
		API:   &recoverAPI{nil},
		Start: &startOffset,
		End:   &endOffset,
	}
	timefilterTest(t, api, timeFilterTestCase{
		validTimes: []time.Time{
			start.Add(time.Minute),
			end,
		},
		invalidTimes: []time.Time{
			now,
			start.Add(time.Minute * -1),
		},
		validRanges: []v1.Range{
			{Start: start, End: end},
			{Start: start.Add(time.Hour * -1), End: end},
			{Start: start, End: end.Add(time.Hour)},
		},
		invalidRanges: []v1.Range{
			{Start: now, End: now},
			{Start: start.Add(time.Hour * -10), End: end.Add(time.Hour * -9)},
		},
	})

}

// For time truncation we need to ensure that any new range's start aligns with a multiple of the step from the overall query start
// otherwise we get into a LOT of trouble with LookbackDelta as the timestamps of the result won't align properly
func TestAbsoluteTimeFilterStepAlignment(t *testing.T) {
	var r v1.Range
	stub := &stubAPI{
		queryRange: func(_ string, rng v1.Range) model.Value {
			r = rng
			return nil
		},
	}
	filterStart, _ := time.Parse(time.RFC3339, "2024-07-01T00:00:00Z")
	api := &AbsoluteTimeFilter{
		API:      stub,
		Start:    filterStart,
		Truncate: true,
	}

	start := time.Unix(1719738435, 0)
	end := time.Unix(1719879274, 0)

	api.QueryRange(context.TODO(), "foo", v1.Range{
		Start: start,
		End:   end,
		Step:  563 * time.Second,
	})

	remainder := r.Start.Sub(start) % r.Step
	if remainder > 0 {
		t.Fatalf("unexpected step misalignment!")
	}
}

// For time truncation we need to ensure that any new range's start aligns with a multiple of the step from the overall query start
// otherwise we get into a LOT of trouble with LookbackDelta as the timestamps of the result won't align properly
func TestRelativeTimeFilterStepAlignment(t *testing.T) {
	var r v1.Range
	stub := &stubAPI{
		queryRange: func(_ string, rng v1.Range) model.Value {
			r = rng
			return nil
		},
	}
	dur, _ := time.ParseDuration("-2h")
	api := &RelativeTimeFilter{
		API:      stub,
		Start:    &dur,
		Truncate: true,
	}

	now := time.Now()
	start := now.Add(-1 * time.Hour * 24)
	end := now

	api.QueryRange(context.TODO(), "foo", v1.Range{
		Start: start,
		End:   end,
		Step:  563 * time.Second,
	})

	remainder := r.Start.Sub(start) % r.Step
	if remainder > 0 {
		t.Fatalf("unexpected step misalignment!")
	}
}
