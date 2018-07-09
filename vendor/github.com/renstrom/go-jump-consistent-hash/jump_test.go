package jump

import (
	"fmt"
	"testing"
)

type testVector struct {
	key     uint64
	buckets int32
	output  int32
}

var jumpTestVectors = []testVector{
	{1, 1, 0},
	{42, 57, 43},
	{0xDEAD10CC, 1, 0},
	{0xDEAD10CC, 666, 361},
	{256, 1024, 520},
	// Test negative values
	{0, -10, 0},
	{0xDEAD10CC, -666, 0},
}

func TestJumpHash(t *testing.T) {
	for _, v := range jumpTestVectors {
		h := Hash(v.key, v.buckets)
		if h != v.output {
			t.Errorf("expected bucket to be %d, got %d", v.output, h)
		}
	}
}

func ExampleHash() {
	fmt.Print(Hash(256, 1024))
	// Output: 520
}

func BenchmarkHash(b *testing.B) {
	for i := 0; i < b.N; i++ {
		Hash(uint64(i), int32(i))
	}
}
