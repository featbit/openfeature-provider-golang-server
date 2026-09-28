package featbit

import (
	"encoding/json"
	"math"

	"github.com/open-feature/go-sdk/openfeature"
)

func booleanValue(raw string) (bool, openfeature.ResolutionError) {
	if raw != "true" && raw != "false" {
		return false, typeMismatch()
	}
	return raw == "true", openfeature.ResolutionError{}
}

func stringValue(raw string) (string, openfeature.ResolutionError) {
	return raw, openfeature.ResolutionError{}
}

func floatValue(raw float64) (float64, openfeature.ResolutionError) {
	if !isFinite(raw) {
		return 0, typeMismatch()
	}
	return raw, openfeature.ResolutionError{}
}

func integerValue(raw string) (int64, openfeature.ResolutionError) {
	value, ok := parseInt64(raw)
	if !ok {
		return 0, typeMismatch()
	}
	return value, openfeature.ResolutionError{}
}

func objectValue(raw any) (any, openfeature.ResolutionError) {
	data, ok := raw.(json.RawMessage)
	if !ok {
		return nil, typeMismatch()
	}
	var value any
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, openfeature.NewParseErrorResolutionError("flag contains invalid JSON")
	}
	switch value.(type) {
	case map[string]any, []any:
		return value, openfeature.ResolutionError{}
	default:
		return nil, typeMismatch()
	}
}

func isFinite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
