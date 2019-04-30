package logging

import (
	"net/url"
	"strconv"
	"testing"
)

func TestFormPrefix(t *testing.T) {
	tooManyValues := make(url.Values)

	for i := 0; i < MaxFormPrefix; i++ {
		k := strconv.Itoa(i)
		tooManyValues[k] = []string{k}
	}

	tests := []struct {
		f url.Values
		v string
	}{
		// Basic one
		{
			f: url.Values(map[string][]string{
				"a": {"a"},
			}),
			v: `a=a`,
		},
		// where a single value is too long
		{
			f: url.Values(map[string][]string{
				"a": {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
			}),
			v: `a=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa`,
		},
		// Where there are too many values
		{
			f: tooManyValues,
		},
	}

	for i, test := range tests {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			v := FormPrefix(test.f)
			if len(v) > MaxFormPrefix {
				t.Fatalf("Value over MaxFormPrefix: expected=%d actual=%d", MaxFormPrefix, len(v))
			}
			if test.v != "" {
				if v != test.v {
					t.Fatalf("Mismatch in values expected=%s actual=%s", test.v, v)
				}
			}
		})
	}
}
