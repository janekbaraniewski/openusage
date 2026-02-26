package copilot

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleLavender
	cfg.ShowClientComposition = true
	cfg.ClientCompositionHeading = "Clients"
	cfg.ShowToolComposition = false
	cfg.ShowActualToolUsage = true
	cfg.ShowLanguageComposition = true
	cfg.ShowCodeStatsComposition = true
	cfg.CodeStatsMetrics = core.CodeStatsConfig{
		LinesAdded:   "composer_lines_added",
		LinesRemoved: "composer_lines_removed",
		FilesChanged: "composer_files_changed",
		Commits:      "scored_commits",
		AIPercent:    "ai_code_percentage",
		Prompts:      "total_prompts",
	}
	cfg.GaugePriority = []string{
		"chat_quota", "completions_quota", "premium_interactions_quota", "context_window",
		"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"chat_quota", "completions_quota", "premium_interactions_quota", "cli_cost", "cost_today", "7d_cost"}, MaxSegments: 6},
		{Label: "Usage", Keys: []string{"context_window", "tokens_today", "7d_tokens", "7d_tool_calls"}, MaxSegments: 4},
		{Label: "Rate", Keys: []string{"gh_core_rpm", "gh_search_rpm", "gh_graphql_rpm"}, MaxSegments: 3},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_prompts"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"cli_input_tokens", "cli_output_tokens", "cli_cache_read_tokens", "cli_cache_write_tokens"}, MaxSegments: 4},
		{Label: "Lines", Keys: []string{"composer_lines_added", "composer_lines_removed", "composer_files_changed", "scored_commits", "total_prompts"}, MaxSegments: 5},
		{
			Label:       "Seats",
			Matcher:     core.DashboardMetricMatcher{Prefix: "org_", Suffix: "_seats"},
			MaxSegments: 3,
		},
	}
	cfg.StandardSectionOrder = []core.DashboardStandardSection{
		core.DashboardSectionHeader,
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionModelBurn,
		core.DashboardSectionClientBurn,
		core.DashboardSectionActualToolUsage,
		core.DashboardSectionLanguageBurn,
		core.DashboardSectionCodeStats,
		core.DashboardSectionOtherData,
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes,
		"model_", "client_", "tool_", "lang_", "org_", "source_", "interface_", "provider_",
		"cli_messages_", "cli_tokens_", "tokens_client_",
	)
	cfg.HideMetricKeys = append(cfg.HideMetricKeys,
		"total_messages", "total_sessions", "total_turns", "total_tool_calls",
		"total_response_chars", "total_reasoning_chars", "total_conversations",
		"cli_messages", "cli_turns", "cli_sessions", "cli_tool_calls", "cli_response_chars", "cli_reasoning_chars",
	)
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys: []string{
				"model_usage", "client_usage", "model_turns", "model_sessions", "model_tool_calls",
				"model_response_chars", "model_reasoning_chars",
			},
		},
		core.DashboardRawGroup{
			Label: "Session",
			Keys: []string{
				"last_session_model", "last_session_client", "last_session_tokens", "last_session_repo",
				"last_session_branch", "last_session_time",
			},
		},
	)
	cfg.MetricLabelOverrides["premium_interactions_quota"] = "Premium Interactions"
	cfg.MetricLabelOverrides["gh_core_rpm"] = "GitHub Core RPM"
	cfg.MetricLabelOverrides["gh_search_rpm"] = "GitHub Search RPM"
	cfg.MetricLabelOverrides["gh_graphql_rpm"] = "GitHub GraphQL RPM"
	cfg.MetricLabelOverrides["cli_input_tokens"] = "CLI Input Tokens"
	cfg.MetricLabelOverrides["cli_output_tokens"] = "CLI Output Tokens"
	cfg.MetricLabelOverrides["cli_cache_read_tokens"] = "CLI Cache Read"
	cfg.MetricLabelOverrides["cli_cache_write_tokens"] = "CLI Cache Write"
	cfg.MetricLabelOverrides["cli_total_tokens"] = "CLI Total Tokens"
	cfg.MetricLabelOverrides["cli_cost"] = "Total Cost"
	cfg.MetricLabelOverrides["cost_today"] = "Cost Today"
	cfg.MetricLabelOverrides["7d_cost"] = "7-Day Cost"
	cfg.MetricLabelOverrides["cli_premium_requests"] = "Premium Requests"
	cfg.MetricLabelOverrides["7d_tokens"] = "7-Day Tokens"
	cfg.MetricLabelOverrides["tokens_today"] = "Today Tokens"
	cfg.MetricLabelOverrides["composer_lines_added"] = "Lines Added"
	cfg.MetricLabelOverrides["composer_lines_removed"] = "Lines Removed"
	cfg.MetricLabelOverrides["composer_files_changed"] = "Files Changed"
	cfg.MetricLabelOverrides["scored_commits"] = "Commits"
	cfg.MetricLabelOverrides["total_prompts"] = "Prompts"
	cfg.CompactMetricLabelOverrides["gh_core_rpm"] = "core"
	cfg.CompactMetricLabelOverrides["gh_search_rpm"] = "search"
	cfg.CompactMetricLabelOverrides["gh_graphql_rpm"] = "graphql"
	cfg.CompactMetricLabelOverrides["premium_interactions_quota"] = "premium"
	cfg.CompactMetricLabelOverrides["cli_input_tokens"] = "cli in"
	cfg.CompactMetricLabelOverrides["cli_output_tokens"] = "cli out"
	cfg.CompactMetricLabelOverrides["cli_cache_read_tokens"] = "cache r"
	cfg.CompactMetricLabelOverrides["cli_cache_write_tokens"] = "cache w"
	cfg.CompactMetricLabelOverrides["cli_cost"] = "cost"
	cfg.CompactMetricLabelOverrides["cost_today"] = "today"
	cfg.CompactMetricLabelOverrides["7d_cost"] = "7d"
	cfg.CompactMetricLabelOverrides["cli_premium_requests"] = "premium"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	cfg.CompactMetricLabelOverrides["composer_lines_added"] = "added"
	cfg.CompactMetricLabelOverrides["composer_lines_removed"] = "removed"
	cfg.CompactMetricLabelOverrides["composer_files_changed"] = "files"
	cfg.CompactMetricLabelOverrides["scored_commits"] = "commits"
	cfg.CompactMetricLabelOverrides["total_prompts"] = "prompts"
	return cfg
}
