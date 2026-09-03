package promapi

import (
	"fmt"
	"math/rand"
	"strconv"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// Any Prometheus-API-compatible downstream (prometheus, VictoriaMetrics, Mimir,
// Thanos, ...) is decoded by this path, so it has to agree with the reference
// decoder every other client of that API uses. Sub-millisecond digits are
// truncated, not rounded, and that has to stay true.
//
// Negative timestamps are excluded: model.Time applies the sign only after
// summing the two halves and so loses it for pre-epoch sub-second times
// (decoding "-59.200" as -58800 rather than -59200). TestUnixSecondsToMillisNegative
// pins the behavior we want there instead.
func TestUnixSecondsToMillisMatchesModelTime(t *testing.T) {
	inputs := []string{
		"0", "1", "1.1", "1.12", "1.123", "1.0", "1.001", "1.010", "1.100",
		"0.999", "0.0001", "1.2345", "1.9999", "1.0009",
		"1700000000", "1700000000.5", "1700000000.123", "1700000000.1235",
		"2147557428.503", "4318490558.624", "9999999999.999",
	}
	rng := rand.New(rand.NewSource(7))
	for i := 0; i < 20000; i++ {
		// Mix millisecond-precision values with deliberately over-precise ones,
		// which is where truncate-vs-round actually diverges.
		ms := rng.Int63n(4_000_000_000_000)
		switch i % 3 {
		case 0:
			inputs = append(inputs, strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64))
		case 1:
			inputs = append(inputs, strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64))
		case 2:
			inputs = append(inputs, fmt.Sprintf("%d.%06d", ms/1000, rng.Intn(1000000)))
		}
	}

	for _, in := range inputs {
		var want model.Time
		if err := want.UnmarshalJSON([]byte(in)); err != nil {
			t.Fatalf("model.Time.UnmarshalJSON(%q): %v", in, err)
		}
		got, err := unixSecondsToMillis([]byte(in))
		if err != nil {
			t.Fatalf("unixSecondsToMillis(%q): %v", in, err)
		}
		if got != int64(want) {
			t.Errorf("unixSecondsToMillis(%q) = %d, model.Time gives %d", in, got, int64(want))
		}
	}
}

// The bug this replaces: reading the timestamp as a float64 and multiplying by
// 1000 does not reliably land on the integer millisecond the text named. The
// product can fall a hair short, and int64() truncates the millisecond away.
// Whether it does depends on the binade, so a sample of current-era timestamps
// looks clean while 2038-era ones are not.
func TestDecodeTimestampExactAcrossEras(t *testing.T) {
	for _, tc := range []struct {
		name   string
		lo, hi int64
	}{
		{"2023-2027", 1_700_000_000_000, 1_800_000_000_000},
		{"2036-2039", 2_100_000_000_000, 2_200_000_000_000},
		{"across the 2038 rollover", 2_147_000_000_000, 2_148_000_000_000},
		{"2096-2099", 4_000_000_000_000, 4_100_000_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(1))
			for i := 0; i < 200_000; i++ {
				ms := tc.lo + rng.Int63n(tc.hi-tc.lo)
				wire := strconv.FormatFloat(float64(ms)/1000, 'f', -1, 64)
				got, err := unixSecondsToMillis([]byte(wire))
				if err != nil {
					t.Fatalf("unixSecondsToMillis(%q): %v", wire, err)
				}
				if got != ms {
					t.Fatalf("unixSecondsToMillis(%q) = %d, want %d (off by %d)", wire, got, ms, got-ms)
				}
			}
		})
	}
}

func TestUnixSecondsToMillisExact(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1", 1000},
		{"1.5", 1500},
		{"1.001", 1001},
		{"1.999", 1999},
		{"1.1", 1100},
		{"1.12", 1120},
		// Over-precise input truncates, as model.Time does.
		{"1.2345", 1234},
		{"1.9999", 1999},
		{"1.0009", 1000},
		{"1700000000.123", 1700000000123},
		// Values the float path decoded a millisecond early.
		{"2147557428.503", 2147557428503},
		{"4318490558.624", 4318490558624},
		// Exponent notation isn't emitted by this API, but must not be rejected.
		{"1.7e9", 1700000000000},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := unixSecondsToMillis([]byte(tc.in))
			if err != nil {
				t.Fatalf("unixSecondsToMillis(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("unixSecondsToMillis(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// Pre-epoch sub-second timestamps: the sign applies to the whole value.
// prometheus/common gets this wrong (it decodes "-59.200" as -58800), which is
// what promclient.hasNegativeFractionalSecond documents; don't inherit that.
func TestUnixSecondsToMillisNegative(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"-1", -1000},
		{"-1.5", -1500},
		{"-59.200", -59200},
		{"-0.001", -1},
		{"-0.5", -500},
	} {
		t.Run(tc.in, func(t *testing.T) {
			got, err := unixSecondsToMillis([]byte(tc.in))
			if err != nil {
				t.Fatalf("unixSecondsToMillis(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("unixSecondsToMillis(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// The decoded SeriesSet must carry the millisecond the wire format named.
func TestDecodeSeriesSetTimestampPrecision(t *testing.T) {
	const wantMs = int64(2147557428503)

	body := []byte(`{"status":"success","data":{"resultType":"matrix","result":[` +
		`{"metric":{"__name__":"foo"},"values":[[2147557428.503,"1"]]}]}}`)

	ss := DecodeSeriesSet(body)
	if err := ss.Err(); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !ss.Next() {
		t.Fatal("no series decoded")
	}
	it := ss.At().Iterator(nil)
	if vt := it.Next(); vt != chunkenc.ValFloat {
		t.Fatalf("first sample type = %v, want float", vt)
	}
	if gotMs, _ := it.At(); gotMs != wantMs {
		t.Errorf("decoded timestamp = %d, want %d (off by %d)", gotMs, wantMs, gotMs-wantMs)
	}
	if it.Next() != chunkenc.ValNone {
		t.Error("expected exactly one sample")
	}
	if ss.Next() {
		t.Error("expected exactly one series")
	}
}
