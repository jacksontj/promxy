package servergroup

import (
	"testing"
)

func TestHistogramMetadataCache_Contains(t *testing.T) {
	var c histogramMetadataCache

	// Empty cache: nothing matches.
	if c.Contains("foo") {
		t.Fatal("empty cache should not match anything")
	}

	// Publish a snapshot and verify lookup.
	m := map[string]struct{}{
		"http_request_duration_seconds": {},
		"rpc_latency_seconds":           {},
	}
	c.names.Store(&m)

	if !c.Contains("http_request_duration_seconds") {
		t.Fatal("cache should match a published histogram name")
	}
	if c.Contains("up") {
		t.Fatal("cache should not match an unrelated name")
	}

	// Nil receiver is safe (server groups without the cache configured
	// won't have one initialized).
	var nilCache *histogramMetadataCache
	if nilCache.Contains("anything") {
		t.Fatal("nil cache should never match")
	}
}
