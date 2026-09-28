package featbit

import (
	"math"
	"strconv"
	"testing"
)

func TestParseInt64(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want int64
	}{
		{"0", 0}, {"-0", 0}, {"42", 42}, {"-42", -42},
		{"42.000", 42}, {"4.2e1", 42}, {"4200e-2", 42}, {"-4.2E1", -42},
		{"9007199254740993", 9007199254740993}, {"9007199254740993.0", 9007199254740993},
		{"9223372036854775807", math.MaxInt64}, {"9223372036854775807.0", math.MaxInt64},
		{"-9223372036854775808", math.MinInt64}, {"-9.223372036854775808e18", math.MinInt64},
		{"0e-1000000000", 0}, {"0e1000000000", 0},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			value, ok := parseInt64(tc.raw)
			if !ok || value != tc.want {
				t.Fatalf("got %d/%v, want %d/true", value, ok, tc.want)
			}
		})
	}
	for _, raw := range []string{
		"", "true", "null", "NaN", "Inf", "1/2", "0x10", `"42"`, "[42]",
		"42.5", "0.0001", "0.99999999999999999999", "1.00000000000000000001",
		"9223372036854775808", "-9223372036854775809", "9223372036854775807.1",
		"1e1000000000", "1e-1000000000", "-1e-1000000000",
	} {
		t.Run("reject/"+raw, func(t *testing.T) {
			if value, ok := parseInt64(raw); ok {
				t.Fatalf("accepted %q as %d", raw, value)
			}
		})
	}
}

func FuzzInt64RoundTrip(f *testing.F) {
	for _, value := range []int64{0, 1, -1, math.MinInt64, math.MaxInt64, 9007199254740993} {
		f.Add(value)
	}
	f.Fuzz(func(t *testing.T, value int64) {
		raw := strconv.FormatInt(value, 10)
		for _, representation := range []string{raw, raw + ".0", raw + "e0"} {
			got, ok := parseInt64(representation)
			if !ok || got != value {
				t.Fatalf("%q parsed as %d/%v, want %d", representation, got, ok, value)
			}
		}
	})
}
