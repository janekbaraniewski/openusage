package codex

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleLavender),
		providerbase.WithGaugePriority(
			"rate_limit_primary", "rate_limit_secondary", "rate_limit_code_review_primary", "context_window",
			"plan_auto_percent_used", "plan_api_percent_used",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"plan_spend", "spend_limit", "individual_spend", "billing_total_cost", "today_cost", "credit_balance"}, MaxSegments: 5},
			core.DashboardCompactRow{Label: "Team", Keys: []string{"team_size", "team_owners"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Usage", Keys: []string{"plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "composer_context_pct"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"requests_today", "total_ai_requests", "composer_sessions", "composer_requests"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Lines", Keys: []string{"composer_lines_added", "composer_lines_removed", "scored_commits", "total_prompts"}, MaxSegments: 4},
		),
		providerbase.WithHideMetricKeys(
			"plan_total_spend_usd", "plan_limit_usd", "plan_included_amount",
			"team_budget_self", "team_budget_others",
			"tool_calls_total", "tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
			"session_input_tokens", "session_output_tokens", "session_cached_tokens", "session_reasoning_tokens",
		),
		providerbase.WithRawGroups(
			core.DashboardRawGroup{
				Label: "Usage Split",
				Keys:  []string{"model_usage", "client_usage", "tool_usage", "language_usage"},
			},
		),
		providerbase.WithMetricLabels(map[string]string{
			"rate_limit_primary":               "Primary Usage",
			"rate_limit_secondary":             "Secondary Usage",
			"rate_limit_code_review_primary":   "Code Review Limit",
			"rate_limit_code_review_secondary": "Code Review Secondary",
			"plan_percent_used":                "Plan Used",
			"plan_auto_percent_used":           "Auto Used",
			"plan_api_percent_used":            "API Used",
			"composer_sessions":                "Sessions",
			"composer_requests":                "Composer Req",
			"scored_commits":                   "Scored Commits",
			"total_prompts":                    "Total Prompts",
			"composer_context_pct":             "Avg Context",
			"ai_deleted_files":                 "AI Deleted",
			"ai_tracked_files":                 "AI Tracked",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"rate_limit_primary":     "primary",
			"rate_limit_secondary":   "secondary",
			"plan_auto_percent_used": "auto",
			"plan_api_percent_used":  "api",
			"requests_today":         "today",
			"total_ai_requests":      "all",
			"composer_sessions":      "sess",
			"composer_requests":      "reqs",
			"credit_balance":         "balance",
			"composer_context_pct":   "ctx",
			"ai_deleted_files":       "deleted",
			"ai_tracked_files":       "tracked",
		}),
	)

	cfg.ClientCompositionIncludeInterfaces = true

	return cfg
}
