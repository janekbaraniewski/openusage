package cursor

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleLavender
	cfg.ShowClientComposition = true
	cfg.GaugePriority = []string{
		"team_budget", "plan_percent_used",
		"plan_auto_percent_used", "plan_api_percent_used",
	}
	cfg.StackedGaugeKeys = map[string]core.StackedGaugeConfig{
		"team_budget": {
			SegmentMetricKeys: []string{"team_budget_self", "team_budget_others"},
			SegmentLabels:     []string{"You", "Team"},
			SegmentColors:     []string{"teal", "peach"},
		},
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"plan_spend", "spend_limit", "individual_spend", "billing_total_cost", "composer_cost", "today_cost"}, MaxSegments: 5},
		{Label: "Team", Keys: []string{"team_size", "team_owners"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"plan_percent_used", "plan_auto_percent_used", "plan_api_percent_used", "composer_context_pct"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"requests_today", "total_ai_requests", "composer_sessions", "composer_requests"}, MaxSegments: 4},
		{Label: "Lines", Keys: []string{"composer_accepted_lines", "composer_suggested_lines", "tab_accepted_lines", "tab_suggested_lines"}, MaxSegments: 4},
		{Label: "Code", Keys: []string{"composer_lines_added", "composer_lines_removed", "composer_files_changed", "scored_commits", "ai_code_percentage"}, MaxSegments: 5},
		{Label: "Files", Keys: []string{"ai_deleted_files", "ai_tracked_files", "total_prompts"}, MaxSegments: 4},
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "source_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "client_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "mode_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "tool_")
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "subagent_")
	cfg.HideMetricKeys = append(cfg.HideMetricKeys, "plan_total_spend_usd", "plan_limit_usd", "plan_included_amount",
		"team_budget_self", "team_budget_others")
	cfg.MetricLabelOverrides["plan_auto_percent_used"] = "Auto Used"
	cfg.MetricLabelOverrides["plan_api_percent_used"] = "API Used"
	cfg.MetricLabelOverrides["ai_code_percentage"] = "AI Code"
	cfg.MetricLabelOverrides["composer_cost"] = "Session Cost"
	cfg.MetricLabelOverrides["today_cost"] = "Today Cost"
	cfg.MetricLabelOverrides["composer_sessions"] = "Sessions"
	cfg.MetricLabelOverrides["composer_requests"] = "Composer Req"
	cfg.MetricLabelOverrides["composer_lines_added"] = "Lines Added"
	cfg.MetricLabelOverrides["composer_lines_removed"] = "Lines Removed"
	cfg.MetricLabelOverrides["scored_commits"] = "Scored Commits"
	cfg.MetricLabelOverrides["total_prompts"] = "Total Prompts"
	cfg.MetricLabelOverrides["billing_total_cost"] = "Billing Total"
	cfg.MetricLabelOverrides["team_size"] = "Team Size"
	cfg.MetricLabelOverrides["team_owners"] = "Team Owners"
	cfg.MetricLabelOverrides["composer_files_changed"] = "Files Changed"
	cfg.MetricLabelOverrides["composer_context_pct"] = "Avg Context"
	cfg.MetricLabelOverrides["ai_deleted_files"] = "AI Deleted"
	cfg.MetricLabelOverrides["ai_tracked_files"] = "AI Tracked"
	cfg.CompactMetricLabelOverrides["plan_auto_percent_used"] = "auto"
	cfg.CompactMetricLabelOverrides["plan_api_percent_used"] = "api"
	cfg.CompactMetricLabelOverrides["requests_today"] = "today"
	cfg.CompactMetricLabelOverrides["total_ai_requests"] = "all"
	cfg.CompactMetricLabelOverrides["composer_sessions"] = "sess"
	cfg.CompactMetricLabelOverrides["composer_requests"] = "reqs"
	cfg.CompactMetricLabelOverrides["composer_cost"] = "total"
	cfg.CompactMetricLabelOverrides["today_cost"] = "today"
	cfg.CompactMetricLabelOverrides["billing_total_cost"] = "billing"
	cfg.CompactMetricLabelOverrides["composer_accepted_lines"] = "comp"
	cfg.CompactMetricLabelOverrides["composer_suggested_lines"] = "comp sug"
	cfg.CompactMetricLabelOverrides["tab_accepted_lines"] = "tab"
	cfg.CompactMetricLabelOverrides["tab_suggested_lines"] = "tab sug"
	cfg.CompactMetricLabelOverrides["composer_lines_added"] = "added"
	cfg.CompactMetricLabelOverrides["composer_lines_removed"] = "removed"
	cfg.CompactMetricLabelOverrides["scored_commits"] = "commits"
	cfg.CompactMetricLabelOverrides["total_prompts"] = "prompts"
	cfg.CompactMetricLabelOverrides["team_size"] = "members"
	cfg.CompactMetricLabelOverrides["team_owners"] = "owners"
	cfg.CompactMetricLabelOverrides["composer_files_changed"] = "files"
	cfg.CompactMetricLabelOverrides["composer_context_pct"] = "ctx"
	cfg.CompactMetricLabelOverrides["ai_deleted_files"] = "deleted"
	cfg.CompactMetricLabelOverrides["ai_tracked_files"] = "tracked"
	cfg.CompactMetricLabelOverrides["ai_code_percentage"] = "ai %"
	return cfg
}
