package core

import (
	"cmp"
	"slices"
	"strings"
)

type RateLimitDisplayMetric struct {
	Key                  string
	LabelKey             string
	UsedPercent          float64
	UsesRemainingPercent bool
	RemainingPercent     float64
}

func ExtractRateLimitDisplayMetrics(metrics map[string]Metric) []RateLimitDisplayMetric {
	out := make([]RateLimitDisplayMetric, 0, len(metrics))
	for key, metric := range metrics {
		labelKey, ok := rateLimitLabelKey(key)
		if !ok {
			continue
		}
		usedPercent := MetricUsedPercent(key, metric)
		if usedPercent < 0 && strings.HasPrefix(key, "rate_limit_") && metric.Unit == "%" && metric.Remaining != nil {
			usedPercent = 100 - *metric.Remaining
		}
		if usedPercent < 0 {
			continue
		}
		entry := RateLimitDisplayMetric{
			Key:         key,
			LabelKey:    labelKey,
			UsedPercent: usedPercent,
		}
		if strings.HasPrefix(key, "rate_limit_") && metric.Unit == "%" && metric.Remaining != nil {
			entry.UsesRemainingPercent = true
			entry.RemainingPercent = *metric.Remaining
		}
		out = append(out, entry)
	}
	slices.SortFunc(out, func(a, b RateLimitDisplayMetric) int {
		return cmp.Compare(a.Key, b.Key)
	})
	return out
}

func FallbackDisplayMetricKeys(metrics map[string]Metric) []string {
	keys := SortedStringKeys(metrics)
	if len(keys) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(keys))
	for _, key := range keys {
		if hasDisplayExcludedPrefix(key) {
			continue
		}
		filtered = append(filtered, key)
	}
	if len(filtered) > 0 {
		return filtered
	}
	return keys
}

func hasDisplayExcludedPrefix(key string) bool {
	for _, prefix := range []string{
		"model_", "client_", "tool_", "source_",
		"usage_model_", "usage_source_", "usage_client_",
		"tokens_client_", "analytics_",
	} {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}

func rateLimitLabelKey(key string) (string, bool) {
	switch key {
	case "rpm", "tpm", "rpd", "tpd":
		return key, true
	}
	if strings.HasPrefix(key, "rate_limit_") {
		labelKey := strings.TrimSpace(strings.TrimPrefix(key, "rate_limit_"))
		return labelKey, labelKey != ""
	}
	return "", false
}
