package roocode

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// DashboardWidget configures how the tile renders metrics for both Roo
// Code and Kilo Code. The two providers share a layout because they share
// the same underlying schema; the wrapping providers pass their own
// color role so tiles stay visually distinct.
//
// Exposed as exported so sibling provider packages (currently
// internal/providers/kilocode) can build a tile with the same metric
// labels and compact-row layout without copy-pasting it.
func DashboardWidget(role core.DashboardColorRole) core.DashboardWidget {
	return dashboardWidget(role)
}

func dashboardWidget(role core.DashboardColorRole) core.DashboardWidget {
	return providerbase.CodingToolDashboard(
		providerbase.WithColorRole(role),
		providerbase.WithGaugePriority(
			"total_tasks", "total_tokens", "total_cost_usd", "cache_hit_ratio",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Tasks",
				Keys:        []string{"total_tasks", "tasks_today", "tasks_7d", "total_requests"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_tokens", "total_input_tokens", "total_output_tokens", "total_cache_read_tokens", "total_cache_write_tokens"},
				MaxSegments: 5,
			},
			core.DashboardCompactRow{
				Label:       "Cost",
				Keys:        []string{"total_cost_usd", "today_cost_usd"},
				MaxSegments: 2,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"total_tasks":              "Tasks",
			"tasks_today":              "Tasks Today",
			"tasks_7d":                 "Tasks 7d",
			"total_requests":           "API Requests",
			"total_tokens":             "Total Tokens",
			"total_input_tokens":       "Input Tokens",
			"total_output_tokens":      "Output Tokens",
			"total_cache_read_tokens":  "Cache Reads",
			"total_cache_write_tokens": "Cache Writes",
			"total_cost_usd":           "Cost",
			"today_cost_usd":           "Today",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_tasks":              "all",
			"tasks_today":              "today",
			"tasks_7d":                 "7d",
			"total_requests":           "reqs",
			"total_tokens":             "total",
			"total_input_tokens":       "in",
			"total_output_tokens":      "out",
			"total_cache_read_tokens":  "cache-r",
			"total_cache_write_tokens": "cache-w",
			"total_cost_usd":           "USD",
			"today_cost_usd":           "today",
		}),
	)
}

func detailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}
