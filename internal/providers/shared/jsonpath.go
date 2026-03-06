package shared

import (
	"encoding/json"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// PathValue traverses a nested map[string]any by the given path segments,
// returning the value at the final key or (nil, false) if any step is missing.
func PathValue(root map[string]any, path ...string) (any, bool) {
	var current any = root
	for _, segment := range path {
		node, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		next, ok := node[segment]
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

// PathMap is like PathValue but returns the result as map[string]any.
func PathMap(root map[string]any, path ...string) (map[string]any, bool) {
	value, ok := PathValue(root, path...)
	if !ok {
		return nil, false
	}
	m, ok := value.(map[string]any)
	return m, ok
}

// PathSlice is like PathValue but returns the result as []any.
func PathSlice(root map[string]any, path ...string) ([]any, bool) {
	value, ok := PathValue(root, path...)
	if !ok {
		return nil, false
	}
	arr, ok := value.([]any)
	return arr, ok
}

// FirstPathString tries multiple JSON paths and returns the first non-empty
// string value found (supports string and json.Number types).
func FirstPathString(root map[string]any, paths ...[]string) string {
	for _, path := range paths {
		if value, ok := PathValue(root, path...); ok {
			switch v := value.(type) {
			case string:
				if trimmed := strings.TrimSpace(v); trimmed != "" {
					return trimmed
				}
			case json.Number:
				if trimmed := strings.TrimSpace(v.String()); trimmed != "" {
					return trimmed
				}
			}
		}
	}
	return ""
}

// FirstPathNumber tries multiple JSON paths and returns the first numeric
// value found (supports float64, float32, int, int64, int32, json.Number, string).
func FirstPathNumber(root map[string]any, paths ...[]string) *float64 {
	for _, path := range paths {
		if value, ok := PathValue(root, path...); ok {
			if parsed, ok := NumberFromAny(value); ok {
				return &parsed
			}
		}
	}
	return nil
}

// NumberFromAny converts various numeric types to float64.
func NumberFromAny(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case int32:
		return float64(v), true
	case json.Number:
		parsed, err := v.Float64()
		return parsed, err == nil
	case string:
		parsed, err := json.Number(strings.TrimSpace(v)).Float64()
		return parsed, err == nil
	default:
		return 0, false
	}
}

// NumberToInt64Ptr converts *float64 to *int64, returning nil for nil input.
func NumberToInt64Ptr(v *float64) *int64 {
	if v == nil {
		return nil
	}
	return core.Int64Ptr(int64(*v))
}

// NumberToFloat64Ptr returns nil for nil input, otherwise a copy of the value.
func NumberToFloat64Ptr(v *float64) *float64 {
	if v == nil {
		return nil
	}
	return core.Float64Ptr(*v)
}
