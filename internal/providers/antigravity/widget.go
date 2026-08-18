package antigravity

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleMauve),
		providerbase.WithGaugeMaxLines(1),
		providerbase.WithGaugePriority("quota", "context_window", "total_tokens"),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Usage", Keys: []string{"quota", "context_window"}, MaxSegments: 2},
			core.DashboardCompactRow{Label: "Session", Keys: []string{"total_tokens", "total_input_tokens", "total_output_tokens"}, MaxSegments: 3},
			core.DashboardCompactRow{Label: "Current", Keys: []string{"current_tokens", "current_input_tokens", "current_output_tokens"}, MaxSegments: 3},
		),
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionModelBurn,
			core.DashboardSectionOtherData,
		),
		providerbase.WithMetricLabels(map[string]string{
			"quota":                      "Quota (Worst)",
			"context_window":             "Context Window",
			"total_tokens":               "Session Tokens",
			"total_input_tokens":         "Session Input Tokens",
			"total_output_tokens":        "Session Output Tokens",
			"current_tokens":             "Current Tokens",
			"current_input_tokens":       "Current Input Tokens",
			"current_output_tokens":      "Current Output Tokens",
			"current_cache_read_tokens":  "Current Cache Read",
			"current_cache_write_tokens": "Current Cache Write",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"quota":                      "quota",
			"context_window":             "ctx",
			"total_tokens":               "all",
			"total_input_tokens":         "in",
			"total_output_tokens":        "out",
			"current_tokens":             "now",
			"current_input_tokens":       "in",
			"current_output_tokens":      "out",
			"current_cache_read_tokens":  "cache read",
			"current_cache_write_tokens": "cache write",
		}),
	)
}
