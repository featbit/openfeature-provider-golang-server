package featbit

import (
	"encoding/json"
	"math"
	"math/big"
	"strconv"
	"strings"
)

func parseInt64(raw string) (int64, bool) {
	if value, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return value, true
	}
	// Accept exact whole decimal/exponent values, without rounding through a float.
	if len(raw) == 0 || !json.Valid([]byte(raw)) || (raw[0] != '-' && (raw[0] < '0' || raw[0] > '9')) {
		return 0, false
	}
	estimate, err := strconv.ParseFloat(raw, 64)
	if err != nil || !isFinite(estimate) || math.Abs(estimate) > math.Exp2(63) {
		return 0, false
	}
	if estimate == 0 {
		// Avoid allocating huge rational denominators for inputs like 1e-1000000000.
		mantissa := raw
		if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
			mantissa = mantissa[:i]
		}
		return 0, !strings.ContainsAny(mantissa, "123456789")
	}
	if math.Abs(estimate) < 1 {
		return 0, false
	}
	exact, ok := new(big.Rat).SetString(raw)
	if !ok || !exact.IsInt() || !exact.Num().IsInt64() {
		return 0, false
	}
	return exact.Num().Int64(), true
}
