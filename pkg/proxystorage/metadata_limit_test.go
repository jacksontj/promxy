package proxystorage

import (
	"fmt"
	"reflect"
	"sort"
	"testing"

	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
)

func metadataFixture(names ...string) map[string][]v1.Metadata {
	m := make(map[string][]v1.Metadata, len(names))
	for _, n := range names {
		m[n] = []v1.Metadata{{Type: v1.MetricTypeCounter, Help: n}}
	}
	return m
}

func sortedKeys(m map[string][]v1.Metadata) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func TestTrimMetadataToLimit(t *testing.T) {
	tests := []struct {
		name  string
		input []string
		limit int
		want  []string
	}{
		{
			name:  "under the limit is untouched",
			input: []string{"b", "a"},
			limit: 5,
			want:  []string{"a", "b"},
		},
		{
			name:  "exactly at the limit is untouched",
			input: []string{"b", "a"},
			limit: 2,
			want:  []string{"a", "b"},
		},
		{
			name:  "over the limit keeps the lowest names",
			input: []string{"delta", "alpha", "charlie", "bravo"},
			limit: 2,
			want:  []string{"alpha", "bravo"},
		},
		{
			name:  "zero drops everything",
			input: []string{"a", "b"},
			limit: 0,
			want:  []string{},
		},
		{
			name:  "negative is clamped to zero rather than panicking",
			input: []string{"a", "b"},
			limit: -1,
			want:  []string{},
		},
		{
			name:  "empty input is a no-op",
			input: nil,
			limit: 3,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := metadataFixture(tt.input...)

			trimMetadataToLimit(m, tt.limit)

			if got := sortedKeys(m); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("kept %v, want %v", got, tt.want)
			}
		})
	}
}

// TestTrimMetadataToLimitIsDeterministic is the point of the sort: Go
// randomizes map iteration order, so a trim that ranged the map directly
// returned a different subset on each call for the same input.
func TestTrimMetadataToLimitIsDeterministic(t *testing.T) {
	names := make([]string, 0, 128)
	for i := 0; i < 128; i++ {
		names = append(names, fmt.Sprintf("metric_%03d", i))
	}

	const limit = 8
	var first []string
	for i := 0; i < 100; i++ {
		m := metadataFixture(names...)
		trimMetadataToLimit(m, limit)

		got := sortedKeys(m)
		if len(got) != limit {
			t.Fatalf("iteration %d: kept %d entries, want %d", i, len(got), limit)
		}
		if first == nil {
			first = got
			continue
		}
		if !reflect.DeepEqual(got, first) {
			t.Fatalf("iteration %d: kept %v, first run kept %v", i, got, first)
		}
	}
}
