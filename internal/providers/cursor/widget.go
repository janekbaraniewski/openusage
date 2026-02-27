package cursor

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.CodingToolDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleLavender),
		providerbase.WithGaugePriority(
			"team_budget", "billing_cycle_progress",
			"plan_auto_percent_used", "plan_api_percent_used",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"plan_spend", "spend_limit", "individual_spend", "billing_total_cost", "today_cost"}, MaxSegments: 5},
			core.DashboardCompactRow{Label: "Team", Keys: []string{"team_size", "team_owners"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Usage", Keys: []string{"plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "composer_context_pct"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"requests_today", "total_ai_requests", "composer_sessions", "composer_requests"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Lines", Keys: []string{"composer_accepted_lines", "composer_suggested_lines", "tab_accepted_lines", "tab_suggested_lines"}, MaxSegments: 4},
		),
		providerbase.WithHideMetricKeys(
			"plan_total_spend_usd", "plan_limit_usd", "plan_included_amount",
			"team_budget_self", "team_budget_others", "composer_cost",
			"agentic_sessions", "non_agentic_sessions", "tool_calls_total",
			"tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
			"composer_files_created", "composer_files_removed",
		),
		providerbase.WithMetricLabels(map[string]string{
			"billing_cycle_progress": "Billing Cycle",
			"plan_percent_used":      "Plan Used",
			"plan_auto_percent_used": "Auto Used",
			"plan_api_percent_used":  "API Used",
			"today_cost":             "Today Cost",
			"composer_sessions":      "Sessions",
			"composer_requests":      "Composer Req",
			"scored_commits":         "Scored Commits",
			"total_prompts":          "Total Prompts",
			"billing_total_cost":     "Billing Total",
			"team_size":              "Team Size",
			"team_owners":            "Team Owners",
			"composer_context_pct":   "Avg Context",
			"ai_deleted_files":       "AI Deleted",
			"ai_tracked_files":       "AI Tracked",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"plan_auto_percent_used":   "auto",
			"plan_api_percent_used":    "api",
			"requests_today":           "today",
			"total_ai_requests":        "all",
			"composer_sessions":        "sess",
			"composer_requests":        "reqs",
			"today_cost":               "today",
			"billing_total_cost":       "billing",
			"composer_accepted_lines":  "comp",
			"composer_suggested_lines": "comp sug",
			"tab_accepted_lines":       "tab",
			"tab_suggested_lines":      "tab sug",
			"team_size":                "members",
			"team_owners":              "owners",
			"composer_context_pct":     "ctx",
			"ai_deleted_files":         "deleted",
			"ai_tracked_files":         "tracked",
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
