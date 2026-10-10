package core

import "strings"

// QuotaConnectHintAttribute is a snapshot attribute a provider sets when its
// quota/usage meters are unavailable until the user connects an extra
// credential (e.g. a console browser session). The TUI renders it as a
// one-line hint in place of the meters.
const QuotaConnectHintAttribute = "quota_connect_hint"

// ratioMetricSuffixes identify percentage metrics that describe a *ratio* of
// observed activity (cache hit rate, tool success rate, token shares) rather
// than consumption of a quota or limit. They are valid gauges when a provider
// opts in via GaugePriority, but they must never be read as "% used".
var ratioMetricSuffixes = []string{
	"_ratio", "_share", "_success_rate", "_hit_rate", "_error_rate",
	"_streamed_percent",
}

// IsRatioMetricKey reports whether key names a ratio metric (see
// ratioMetricSuffixes). Rate-limit keys (rate_limit_*) are quotas, not
// ratios, and are never classified as ratios.
func IsRatioMetricKey(key string) bool {
	if strings.HasPrefix(key, "rate_limit_") {
		return false
	}
	for _, suffix := range ratioMetricSuffixes {
		if strings.HasSuffix(key, suffix) {
			return true
		}
	}
	return false
}

func MetricUsedPercent(key string, m Metric) float64 {
	if key == "context_window" {
		return -1
	}
	if m.Unit == "%" && m.Used != nil {
		return *m.Used
	}
	if m.Limit != nil && m.Remaining != nil && *m.Limit > 0 {
		return (*m.Limit - *m.Remaining) / *m.Limit * 100
	}
	if m.Limit != nil && m.Used != nil && *m.Limit > 0 {
		return *m.Used / *m.Limit * 100
	}
	return -1
}
