package core

import (
	"sort"
	"strings"

	"github.com/samber/lo"
)

func SortedCompactStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	compact := lo.FilterMap(values, func(value string, _ int) (string, bool) {
		trimmed := strings.TrimSpace(value)
		return trimmed, trimmed != ""
	})
	if len(compact) == 0 {
		return nil
	}
	result := lo.Uniq(compact)
	sort.Strings(result)
	return result
}

func SortedStringKeys[V any](values map[string]V) []string {
	if len(values) == 0 {
		return nil
	}
	keys := lo.Keys(values)
	sort.Strings(keys)
	return keys
}

func SortedTimePoints(values map[string]float64) []TimePoint {
	if len(values) == 0 {
		return nil
	}

	keys := SortedStringKeys(values)
	points := make([]TimePoint, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		points = append(points, TimePoint{Date: key, Value: values[key]})
	}
	return points
}
