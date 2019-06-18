package promclient

import (
	"context"
	"fmt"
	"testing"
	"time"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
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
