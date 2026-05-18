package crush

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleSky),
		providerbase.WithGaugePriority(
			"total_sessions", "total_tokens",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Sessions",
				Keys:        []string{"total_sessions", "sessions_today", "sessions_7d"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Cost",
				Keys:        []string{"total_cost_usd"},
				MaxSegments: 2,
			},
			core.DashboardCompactRow{
				Label:       "Projects",
				Keys:        []string{"total_projects"},
				MaxSegments: 2,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"total_sessions":      "Sessions",
			"total_tokens":        "Total Tokens",
			"total_input_tokens":  "Input Tokens",
			"total_output_tokens": "Output Tokens",
			"total_cost_usd":      "Cost",
			"sessions_today":      "Sessions Today",
			"sessions_7d":         "Sessions 7d",
			"total_projects":      "Projects",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_sessions":      "all",
			"sessions_today":      "today",
			"sessions_7d":         "7d",
			"total_tokens":        "total",
			"total_input_tokens":  "in",
			"total_output_tokens": "out",
			"total_cost_usd":      "USD",
			"total_projects":      "count",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
