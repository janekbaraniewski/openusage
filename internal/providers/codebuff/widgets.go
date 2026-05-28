package codebuff

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleRosewater),
		providerbase.WithGaugePriority(
			"total_chats", "total_tokens",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Chats",
				Keys:        []string{"total_chats", "chats_today", "chats_7d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Credits",
				Keys:        []string{"total_credits"},
				MaxSegments: 2,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"total_chats":         "Chats",
			"total_messages":      "Messages",
			"total_tokens":        "Total Tokens",
			"total_input_tokens":  "Input Tokens",
			"total_output_tokens": "Output Tokens",
			"total_cache_read":    "Cache Read",
			"total_cache_write":   "Cache Write",
			"total_credits":       "Credits",
			"chats_today":         "Chats Today",
			"chats_7d":            "Chats 7d",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_chats":         "all",
			"chats_today":         "today",
			"chats_7d":            "7d",
			"total_tokens":        "total",
			"total_input_tokens":  "in",
			"total_output_tokens": "out",
			"total_credits":       "credits",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
