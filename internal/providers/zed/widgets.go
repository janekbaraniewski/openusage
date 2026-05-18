package zed

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleSky),
		providerbase.WithGaugePriority(
			"total_threads", "total_tokens",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Threads",
				Keys:        []string{"total_threads", "threads_today", "threads_7d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens", "total_reasoning_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Cache",
				Keys:        []string{"total_cache_read", "total_cache_write"},
				MaxSegments: 2,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"total_threads":          "Threads",
			"total_tokens":           "Total Tokens",
			"total_input_tokens":     "Input Tokens",
			"total_output_tokens":    "Output Tokens",
			"total_cache_read":       "Cache Read",
			"total_cache_write":      "Cache Write",
			"total_reasoning_tokens": "Reasoning",
			"threads_today":          "Threads Today",
			"threads_7d":             "Threads 7d",
			"total_messages":         "Messages",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_threads":          "all",
			"threads_today":          "today",
			"threads_7d":             "7d",
			"total_tokens":           "total",
			"total_input_tokens":     "in",
			"total_output_tokens":    "out",
			"total_reasoning_tokens": "reason",
			"total_cache_read":       "read",
			"total_cache_write":      "write",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
