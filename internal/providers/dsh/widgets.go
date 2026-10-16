package dsh

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleSapphire),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Sessions",
				Keys:        []string{"sessions_7d", "sessions_today", "total_sessions"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Activity",
				Keys:        []string{"total_messages", "total_tool_calls"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens", "total_cache_read"},
				MaxSegments: 4,
			},
		),
		// provider_* metrics feed the upstream-provider section; without
		// hiding they also show up as raw "Provider Zai Input Tokens" lines.
		providerbase.WithHideMetricPrefixes("provider_"),
		// window_* duplicates the tile's "N reqs · M tok in <window>" activity
		// line; the cache-write and reasoning totals are noise while zero.
		providerbase.WithHideMetricKeys("window_requests", "window_tokens"),
		providerbase.WithSuppressZeroMetricKeys("total_cache_write", "total_reasoning_tokens"),
		providerbase.WithMetricLabels(map[string]string{
			"total_sessions":         "Sessions",
			"sessions_today":         "Sessions Today",
			"sessions_7d":            "Sessions 7d",
			"total_messages":         "Messages",
			"total_tool_calls":       "Tool Calls",
			"total_tokens":           "Total Tokens",
			"total_input_tokens":     "Input Tokens",
			"total_output_tokens":    "Output Tokens",
			"total_cache_read":       "Cache Read",
			"total_cache_write":      "Cache Write",
			"total_reasoning_tokens": "Reasoning Tokens",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_sessions":         "all",
			"sessions_today":         "today",
			"sessions_7d":            "7d",
			"total_messages":         "msgs",
			"total_tool_calls":       "tools",
			"total_tokens":           "total",
			"total_input_tokens":     "in",
			"total_output_tokens":    "out",
			"total_cache_read":       "cache r",
			"total_cache_write":      "cache w",
			"total_reasoning_tokens": "reason",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
