package cursor

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleLavender),
		providerbase.WithGaugeMaxLines(4),
		providerbase.WithGaugePriority(
			"context_window",
			"quota",
			"plan_auto_percent_used",
			"plan_api_percent_used",
			"plan_percent_used",
			"billing_cycle_progress",
			"team_budget",
			"cache_hit_ratio",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Session",
				Keys:        []string{"context_window", "total_tokens", "total_input_tokens", "total_output_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Current",
				Keys:        []string{"current_tokens", "current_input_tokens", "current_output_tokens", "current_cache_read_tokens"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Plan",
				Keys:        []string{"plan_auto_percent_used", "plan_api_percent_used", "plan_percent_used", "composer_context_pct"},
				MaxSegments: 4,
			},
			core.DashboardCompactRow{
				Label:       "Credits",
				Keys:        []string{"plan_spend", "spend_limit", "individual_spend", "billing_total_cost", "today_cost"},
				MaxSegments: 5,
			},
			core.DashboardCompactRow{
				Label:       "Activity",
				Keys:        []string{"requests_today", "total_ai_requests", "composer_sessions", "composer_requests"},
				MaxSegments: 4,
			},
		),
		providerbase.WithHideMetricKeys(
			"plan_spend", "plan_total_spend_usd", "plan_limit_usd", "plan_included_amount",
			"team_budget_self", "team_budget_others", "composer_cost",
			"agentic_sessions", "non_agentic_sessions", "tool_calls_total",
			"tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
			"composer_files_created", "composer_files_removed",
		),
		providerbase.WithMetricLabels(map[string]string{
			"context_window":             "Context Window",
			"quota":                      "Cursor Quota",
			"total_tokens":               "Session Tokens",
			"total_input_tokens":         "Session Input",
			"total_output_tokens":        "Session Output",
			"current_tokens":             "Current Tokens",
			"current_input_tokens":       "Current Input",
			"current_output_tokens":      "Current Output",
			"current_cache_read_tokens":  "Cache Read",
			"current_cache_write_tokens": "Cache Write",
			"billing_cycle_progress":     "Billing Cycle",
			"plan_percent_used":          "Plan Total",
			"plan_auto_percent_used":     "Auto (Grok/Cursor)",
			"plan_api_percent_used":      "API Usage",
			"today_cost":                 "Today Cost",
			"composer_sessions":          "Sessions",
			"composer_requests":          "Composer Req",
			"scored_commits":             "Scored Commits",
			"total_prompts":              "Total Prompts",
			"billing_total_cost":         "Billing Total",
			"team_size":                  "Team Size",
			"team_owners":                "Team Owners",
			"composer_context_pct":       "Avg Context",
			"ai_deleted_files":           "AI Deleted",
			"ai_tracked_files":           "AI Tracked",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"context_window":             "ctx",
			"quota":                      "quota",
			"total_tokens":               "all",
			"total_input_tokens":         "in",
			"total_output_tokens":        "out",
			"current_tokens":             "now",
			"current_input_tokens":       "in",
			"current_output_tokens":      "out",
			"current_cache_read_tokens":  "read",
			"current_cache_write_tokens": "write",
			"plan_auto_percent_used":     "auto",
			"plan_api_percent_used":      "api",
			"plan_percent_used":          "plan",
			"requests_today":             "today",
			"total_ai_requests":          "all",
			"composer_sessions":          "sess",
			"composer_requests":          "reqs",
			"today_cost":                 "today",
			"billing_total_cost":         "billing",
			"composer_accepted_lines":    "comp",
			"composer_suggested_lines":   "comp sug",
			"tab_accepted_lines":         "tab",
			"tab_suggested_lines":        "tab sug",
			"team_size":                  "members",
			"team_owners":                "owners",
			"composer_context_pct":       "ctx",
			"ai_deleted_files":           "deleted",
			"ai_tracked_files":           "tracked",
		}),
	)

	cfg.ClientCompositionIncludeInterfaces = true
	cfg.StackedGaugeKeys = map[string]core.StackedGaugeConfig{
		"team_budget": {
			SegmentMetricKeys: []string{"team_budget_self", "team_budget_others"},
			SegmentLabels:     []string{"You", "Team"},
			SegmentColors:     []string{"teal", "peach"},
		},
	}

	return cfg
}
