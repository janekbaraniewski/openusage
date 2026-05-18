package amp

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// dashboardWidget describes how Amp's metrics render in the dashboard tile.
// We treat Amp as a coding-tool provider so it picks up the standard client/
// language/code-stats composition panels and the shared section order.
func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRolePeach),
		providerbase.WithGaugePriority(
			"plan_percent_used", "today_cost", "total_cost",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Credits",
				Keys:        []string{"total_cost", "today_cost", "credit_balance"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Tokens",
				Keys:        []string{"total_input_tokens", "total_output_tokens", "total_cache_read_tokens", "total_cache_write_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Activity",
				Keys:        []string{"total_messages", "total_sessions", "messages_today"},
				MaxSegments: 4,
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"total_cost":               "Total Cost",
			"today_cost":               "Today Cost",
			"total_input_tokens":       "Input Tokens",
			"total_output_tokens":      "Output Tokens",
			"total_cache_read_tokens":  "Cache Read",
			"total_cache_write_tokens": "Cache Write",
			"total_messages":           "All-Time Msgs",
			"total_sessions":           "All-Time Sessions",
			"messages_today":           "Today Messages",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"total_cost":               "all",
			"today_cost":               "today",
			"total_input_tokens":       "in",
			"total_output_tokens":      "out",
			"total_cache_read_tokens":  "cache-r",
			"total_cache_write_tokens": "cache-w",
			"total_messages":           "msgs",
			"total_sessions":           "sess",
			"messages_today":           "msgs/d",
		}),
		providerbase.WithRawGroups(
			core.DashboardRawGroup{
				Label: "Tool",
				Keys:  []string{"data_dir", "threads_dir", "ledger_path", "thread_count"},
			},
		),
	)
	return cfg
}
