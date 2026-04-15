package shared

import (
	"fmt"
	"sort"
	"strings"
)

func NormalizeLooseModelName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "unknown"
	}
	return name
}

func NormalizeLooseClientName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return "Other"
	}
	return name
}

func SanitizeMetricName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}

	var b strings.Builder
	lastUnderscore := false
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastUnderscore = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}

	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "unknown"
	}
	return out
}

func SummarizeShareUsage(values map[string]float64, maxItems int, normalizeLabel func(string) string) string {
	type item struct {
		name  string
		value float64
	}
	var (
		list  []item
		total float64
	)
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
		total += value
	}
	if len(list) == 0 || total <= 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	if normalizeLabel == nil {
		normalizeLabel = strings.TrimSpace
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f%%", normalizeLabel(entry.name), entry.value/total*100))
	}
	return strings.Join(parts, ", ")
}

func SummarizeCountUsage(values map[string]float64, unit string, maxItems int, normalizeLabel func(string) string) string {
	type item struct {
		name  string
		value float64
	}
	var list []item
	for name, value := range values {
		if value <= 0 {
			continue
		}
		list = append(list, item{name: name, value: value})
	}
	if len(list) == 0 {
		return ""
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].value != list[j].value {
			return list[i].value > list[j].value
		}
		return list[i].name < list[j].name
	})
	if maxItems > 0 && len(list) > maxItems {
		list = list[:maxItems]
	}
	if normalizeLabel == nil {
		normalizeLabel = strings.TrimSpace
	}
	parts := make([]string, 0, len(list))
	for _, entry := range list {
		parts = append(parts, fmt.Sprintf("%s: %.0f %s", normalizeLabel(entry.name), entry.value, unit))
	}
	return strings.Join(parts, ", ")
}
