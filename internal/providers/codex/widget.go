package codex

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleLavender
	cfg.ShowClientComposition = true
	cfg.ClientCompositionHeading = "Clients"
	cfg.ClientCompositionIncludeInterfaces = true
	cfg.ShowToolComposition = false
	cfg.ShowLanguageComposition = true
	cfg.ShowCodeStatsComposition = true
	cfg.ShowActualToolUsage = true
	cfg.CodeStatsMetrics = core.CodeStatsConfig{
		LinesAdded:   "composer_lines_added",
		LinesRemoved: "composer_lines_removed",
		FilesChanged: "composer_files_changed",
		Commits:      "scored_commits",
		AIPercent:    "ai_code_percentage",
		Prompts:      "total_prompts",
	}
	cfg.GaugePriority = []string{
		"rate_limit_primary", "rate_limit_secondary", "rate_limit_code_review_primary", "context_window",
		"plan_auto_percent_used", "plan_api_percent_used",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"plan_spend", "spend_limit", "individual_spend", "billing_total_cost", "today_cost", "credit_balance"}, MaxSegments: 5},
		{Label: "Team", Keys: []string{"team_size", "team_owners"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "composer_context_pct"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"requests_today", "total_ai_requests", "composer_sessions", "composer_requests"}, MaxSegments: 4},
		{Label: "Lines", Keys: []string{"composer_lines_added", "composer_lines_removed", "scored_commits", "total_prompts"}, MaxSegments: 4},
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "source_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "client_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "mode_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "interface_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "subagent_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "lang_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "tool_")
	cfg.HideMetricKeys = append(cfg.HideMetricKeys,
		"plan_total_spend_usd", "plan_limit_usd", "plan_included_amount",
		"team_budget_self", "team_budget_others",
		"tool_calls_total", "tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
		"session_input_tokens", "session_output_tokens", "session_cached_tokens", "session_reasoning_tokens",
	)
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys:  []string{"model_usage", "client_usage", "tool_usage", "language_usage"},
		},
	)
	cfg.MetricLabelOverrides["rate_limit_primary"] = "Primary Usage"
	cfg.MetricLabelOverrides["rate_limit_secondary"] = "Secondary Usage"
	cfg.MetricLabelOverrides["rate_limit_code_review_primary"] = "Code Review Limit"
	cfg.MetricLabelOverrides["rate_limit_code_review_secondary"] = "Code Review Secondary"
	cfg.MetricLabelOverrides["plan_percent_used"] = "Plan Used"
	cfg.MetricLabelOverrides["plan_auto_percent_used"] = "Auto Used"
	cfg.MetricLabelOverrides["plan_api_percent_used"] = "API Used"
	cfg.MetricLabelOverrides["ai_code_percentage"] = "AI Code"
	cfg.MetricLabelOverrides["composer_sessions"] = "Sessions"
	cfg.MetricLabelOverrides["composer_requests"] = "Composer Req"
	cfg.MetricLabelOverrides["composer_lines_added"] = "Lines Added"
	cfg.MetricLabelOverrides["composer_lines_removed"] = "Lines Removed"
	cfg.MetricLabelOverrides["scored_commits"] = "Scored Commits"
	cfg.MetricLabelOverrides["total_prompts"] = "Total Prompts"
	cfg.MetricLabelOverrides["composer_context_pct"] = "Avg Context"
	cfg.MetricLabelOverrides["ai_deleted_files"] = "AI Deleted"
	cfg.MetricLabelOverrides["ai_tracked_files"] = "AI Tracked"
	cfg.CompactMetricLabelOverrides["rate_limit_primary"] = "primary"
	cfg.CompactMetricLabelOverrides["rate_limit_secondary"] = "secondary"
	cfg.CompactMetricLabelOverrides["plan_auto_percent_used"] = "auto"
	cfg.CompactMetricLabelOverrides["plan_api_percent_used"] = "api"
	cfg.CompactMetricLabelOverrides["requests_today"] = "today"
	cfg.CompactMetricLabelOverrides["total_ai_requests"] = "all"
	cfg.CompactMetricLabelOverrides["composer_sessions"] = "sess"
	cfg.CompactMetricLabelOverrides["composer_requests"] = "reqs"
	cfg.CompactMetricLabelOverrides["credit_balance"] = "balance"
	cfg.CompactMetricLabelOverrides["composer_lines_added"] = "added"
	cfg.CompactMetricLabelOverrides["composer_lines_removed"] = "removed"
	cfg.CompactMetricLabelOverrides["scored_commits"] = "commits"
	cfg.CompactMetricLabelOverrides["total_prompts"] = "prompts"
	cfg.CompactMetricLabelOverrides["composer_context_pct"] = "ctx"
	cfg.CompactMetricLabelOverrides["ai_deleted_files"] = "deleted"
	cfg.CompactMetricLabelOverrides["ai_tracked_files"] = "tracked"
	cfg.CompactMetricLabelOverrides["ai_code_percentage"] = "ai %"
	return cfg
}
