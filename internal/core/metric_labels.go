package core

import "strings"

var prettifyKeyOverrides = map[string]string{
	"plan_percent_used":    "Plan Used",
	"plan_total_spend_usd": "Total Plan Spend",
	"spend_limit":          "Spend Limit",
	"individual_spend":     "Individual Spend",
	"context_window":       "Context Window",
}

func MetricLabel(widget DashboardWidget, key string) string {
	if widget.MetricLabelOverrides != nil {
		if label, ok := widget.MetricLabelOverrides[key]; ok && label != "" {
			return NormalizeMetricLabel(label)
		}
	}
	return NormalizeMetricLabel(PrettifyMetricKey(key))
}

func NormalizeMetricLabel(label string) string {
	label = strings.TrimSpace(label)
	if label == "" {
		return label
	}

	replacements := []struct {
		old string
		new string
	}{
		{"5h Block", "Usage 5h"},
		{"5-Hour Usage", "Usage 5h"},
		{"5h Usage", "Usage 5h"},
		{"7-Day Usage", "Usage 7d"},
		{"7d Usage", "Usage 7d"},
	}
	for _, repl := range replacements {
		label = strings.ReplaceAll(label, repl.old, repl.new)
	}
	return label
}

func PrettifyMetricKey(key string) string {
	if label, ok := prettifyKeyOverrides[key]; ok {
		return label
	}
	parts := strings.Split(key, "_")
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	result := strings.Join(parts, " ")
	for _, pair := range [][2]string{
		{"Usd", "USD"}, {"Rpm", "RPM"}, {"Tpm", "TPM"},
		{"Rpd", "RPD"}, {"Tpd", "TPD"}, {"Api", "API"},
	} {
		result = strings.ReplaceAll(result, pair[0], pair[1])
	}
	return result
}
