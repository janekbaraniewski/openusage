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

func PrettifyUsageMetricLabel(key string, widget DashboardWidget) string {
	lastUnderscore := strings.LastIndex(key, "_")
	if lastUnderscore > 0 && lastUnderscore < len(key)-1 {
		suffix := key[lastUnderscore+1:]
		prefix := key[:lastUnderscore]
		if suffix == strings.ToUpper(suffix) && len(suffix) > 1 {
			return prettifyModelHyphens(prefix) + " " + titleCase(suffix)
		}
	}
	return MetricLabel(widget, key)
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

func ClassifyDetailMetric(key string, m Metric, widget DashboardWidget, details DetailWidget) (group, label string, order int) {
	if override, ok := widget.MetricGroupOverrides[key]; ok && override.Group != "" {
		label = override.Label
		if label == "" {
			label = MetricLabel(widget, key)
		}
		label = NormalizeMetricLabel(label)
		order = override.Order
		if order <= 0 {
			order = detailMetricGroupOrder(details, override.Group, 4)
		}
		return override.Group, label, order
	}

	group = string(InferMetricGroup(key, m))
	label = MetricLabel(widget, key)
	switch group {
	case string(MetricGroupUsage):
		if strings.HasPrefix(key, "rate_limit_") {
			label = MetricLabel(widget, strings.TrimPrefix(key, "rate_limit_"))
		} else if m.Remaining != nil && m.Limit != nil && m.Unit != "%" && m.Unit != "USD" {
			label = PrettifyUsageMetricLabel(key, widget)
		}
		order = detailMetricGroupOrder(details, group, 1)
	case string(MetricGroupSpending):
		if strings.HasPrefix(key, "model_") &&
			!strings.HasSuffix(key, "_input_tokens") &&
			!strings.HasSuffix(key, "_output_tokens") {
			label = strings.TrimPrefix(key, "model_")
		}
		order = detailMetricGroupOrder(details, group, 2)
	case string(MetricGroupTokens):
		if strings.HasPrefix(key, "session_") {
			label = MetricLabel(widget, strings.TrimPrefix(key, "session_"))
		}
		order = detailMetricGroupOrder(details, group, 3)
	default:
		order = detailMetricGroupOrder(details, string(MetricGroupActivity), 4)
		group = string(MetricGroupActivity)
	}
	return group, label, order
}

func detailMetricGroupOrder(details DetailWidget, group string, fallback int) int {
	if order := details.SectionOrder(group); order > 0 {
		return order
	}
	return fallback
}

func prettifyModelHyphens(name string) string {
	parts := strings.Split(name, "-")
	for i, p := range parts {
		if len(p) == 0 {
			continue
		}
		if p[0] >= '0' && p[0] <= '9' {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

func titleCase(s string) string {
	if len(s) <= 1 {
		return s
	}
	return strings.ToUpper(s[:1]) + strings.ToLower(s[1:])
}
