package core

import (
	"maps"
	"slices"
	"strings"
)

func SortedTimePoints(values map[string]float64) []TimePoint {
	if len(values) == 0 {
		return nil
	}

	keys := slices.Sorted(maps.Keys(values))
	points := make([]TimePoint, 0, len(keys))
	for _, key := range keys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		points = append(points, TimePoint{Date: key, Value: values[key]})
	}
	return points
}
