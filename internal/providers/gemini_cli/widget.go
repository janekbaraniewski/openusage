package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleBlue
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
	cfg.GaugeMaxLines = 1
	cfg.GaugePriority = []string{
		"quota", "quota_pro", "quota_flash", "context_window", "tokens_today", "7d_tokens", "messages_today", "sessions_today", "tool_calls_today",
		"client_cli_total_tokens", "client_cli_input_tokens",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Usage", Keys: []string{"quota", "quota_models_exhausted", "quota_models_low", "quota_models_tracked"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_conversations"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"tokens_today", "7d_tokens", "today_input_tokens", "today_output_tokens"}, MaxSegments: 4},
		{Label: "Today Tok", Keys: []string{"today_cached_tokens", "today_reasoning_tokens", "today_tool_tokens"}, MaxSegments: 3},
		{Label: "7d Tok", Keys: []string{"7d_input_tokens", "7d_output_tokens", "7d_cached_tokens", "7d_reasoning_tokens", "7d_tool_tokens"}, MaxSegments: 5},
		{Label: "Totals", Keys: []string{"total_input_tokens", "total_output_tokens", "total_cached_tokens", "total_reasoning_tokens", "total_tool_tokens", "total_tokens"}, MaxSegments: 6},
		{Label: "Tools", Keys: []string{"tool_calls_total", "tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate"}, MaxSegments: 5},
		{Label: "Efficiency", Keys: []string{"cache_efficiency", "reasoning_share", "tool_token_share", "avg_tokens_per_turn", "avg_tools_per_session"}, MaxSegments: 5},
		{Label: "Lines", Keys: []string{"composer_lines_added", "composer_lines_removed", "composer_files_changed", "scored_commits", "total_prompts"}, MaxSegments: 5},
	}
	cfg.StandardSectionOrder = []core.DashboardStandardSection{
		core.DashboardSectionHeader,
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionModelBurn,
		core.DashboardSectionClientBurn,
		core.DashboardSectionActualToolUsage,
		core.DashboardSectionLanguageBurn,
		core.DashboardSectionCodeStats,
		core.DashboardSectionDailyUsage,
		core.DashboardSectionOtherData,
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes,
		"model_", "client_", "tool_", "lang_", "tokens_model_", "tokens_client_",
	)
	cfg.HideMetricKeys = append(cfg.HideMetricKeys,
		"total_messages", "total_sessions", "total_turns", "total_tool_calls",
		"client_cli_messages", "client_cli_turns", "client_cli_tool_calls",
		"tool_calls_success", "tool_calls_failed",
		"composer_lines_added", "composer_lines_removed", "composer_files_changed",
		"scored_commits", "total_prompts", "tool_calls_total", "tool_completed",
		"tool_errored", "tool_cancelled", "tool_success_rate",
	)
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys:  []string{"model_usage", "client_usage", "tool_usage", "language_usage"},
		},
	)
	cfg.CompactMetricLabelOverrides["client_cli_input_tokens"] = "cli in"
	cfg.CompactMetricLabelOverrides["client_cli_total_tokens"] = "cli total"
	cfg.CompactMetricLabelOverrides["tokens_today"] = "today tok"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	cfg.CompactMetricLabelOverrides["quota"] = "all"
	cfg.CompactMetricLabelOverrides["quota_pro"] = "pro"
	cfg.CompactMetricLabelOverrides["quota_flash"] = "flash"
	cfg.CompactMetricLabelOverrides["today_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["today_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["today_cached_tokens"] = "cached"
	cfg.CompactMetricLabelOverrides["today_reasoning_tokens"] = "reason"
	cfg.CompactMetricLabelOverrides["today_tool_tokens"] = "tools"
	cfg.CompactMetricLabelOverrides["7d_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["7d_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["7d_cached_tokens"] = "cached"
	cfg.CompactMetricLabelOverrides["7d_reasoning_tokens"] = "reason"
	cfg.CompactMetricLabelOverrides["7d_tool_tokens"] = "tools"
	cfg.CompactMetricLabelOverrides["total_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["total_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["total_cached_tokens"] = "cached"
	cfg.CompactMetricLabelOverrides["total_reasoning_tokens"] = "reason"
	cfg.CompactMetricLabelOverrides["total_tool_tokens"] = "tools"
	cfg.CompactMetricLabelOverrides["total_tokens"] = "all"
	cfg.CompactMetricLabelOverrides["avg_tokens_per_turn"] = "tok/turn"
	cfg.CompactMetricLabelOverrides["avg_tools_per_session"] = "tools/sess"
	cfg.CompactMetricLabelOverrides["cache_efficiency"] = "cache %"
	cfg.CompactMetricLabelOverrides["reasoning_share"] = "reason %"
	cfg.CompactMetricLabelOverrides["tool_token_share"] = "tool %"
	cfg.CompactMetricLabelOverrides["quota_models_exhausted"] = "exhausted"
	cfg.CompactMetricLabelOverrides["tool_calls_total"] = "all"
	cfg.CompactMetricLabelOverrides["tool_completed"] = "ok"
	cfg.CompactMetricLabelOverrides["tool_errored"] = "err"
	cfg.CompactMetricLabelOverrides["tool_cancelled"] = "cancel"
	cfg.CompactMetricLabelOverrides["tool_success_rate"] = "ok %"
	cfg.CompactMetricLabelOverrides["composer_lines_added"] = "added"
	cfg.CompactMetricLabelOverrides["composer_lines_removed"] = "removed"
	cfg.CompactMetricLabelOverrides["composer_files_changed"] = "files"
	cfg.CompactMetricLabelOverrides["scored_commits"] = "commits"
	cfg.CompactMetricLabelOverrides["total_prompts"] = "prompts"
	cfg.MetricLabelOverrides["client_cli_input_tokens"] = "CLI Input Tokens"
	cfg.MetricLabelOverrides["client_cli_total_tokens"] = "CLI Total Tokens"
	cfg.MetricLabelOverrides["total_turns"] = "All-Time Turns"
	cfg.MetricLabelOverrides["total_tool_calls"] = "All-Time Tool Calls"
	cfg.MetricLabelOverrides["tokens_today"] = "Today Tokens"
	cfg.MetricLabelOverrides["7d_tokens"] = "7-Day Tokens"
	cfg.MetricLabelOverrides["quota"] = "Usage (Worst Model)"
	cfg.MetricLabelOverrides["quota_pro"] = "Pro Usage"
	cfg.MetricLabelOverrides["quota_flash"] = "Flash Usage"
	cfg.MetricLabelOverrides["quota_models_tracked"] = "Tracked Usage Models"
	cfg.MetricLabelOverrides["quota_models_low"] = "Low Usage Models"
	cfg.MetricLabelOverrides["quota_models_exhausted"] = "Exhausted Usage Models"
	cfg.MetricLabelOverrides["today_input_tokens"] = "Today Input Tokens"
	cfg.MetricLabelOverrides["today_output_tokens"] = "Today Output Tokens"
	cfg.MetricLabelOverrides["today_cached_tokens"] = "Today Cached Tokens"
	cfg.MetricLabelOverrides["today_reasoning_tokens"] = "Today Reasoning Tokens"
	cfg.MetricLabelOverrides["today_tool_tokens"] = "Today Tool Tokens"
	cfg.MetricLabelOverrides["7d_input_tokens"] = "7-Day Input Tokens"
	cfg.MetricLabelOverrides["7d_output_tokens"] = "7-Day Output Tokens"
	cfg.MetricLabelOverrides["7d_cached_tokens"] = "7-Day Cached Tokens"
	cfg.MetricLabelOverrides["7d_reasoning_tokens"] = "7-Day Reasoning Tokens"
	cfg.MetricLabelOverrides["7d_tool_tokens"] = "7-Day Tool Tokens"
	cfg.MetricLabelOverrides["cache_efficiency"] = "Cache Efficiency"
	cfg.MetricLabelOverrides["reasoning_share"] = "Reasoning Share"
	cfg.MetricLabelOverrides["tool_token_share"] = "Tool Token Share"
	cfg.MetricLabelOverrides["tool_calls_total"] = "All-Time Tool Calls"
	cfg.MetricLabelOverrides["tool_completed"] = "Tool Success"
	cfg.MetricLabelOverrides["tool_errored"] = "Tool Errors"
	cfg.MetricLabelOverrides["tool_cancelled"] = "Tool Cancelled"
	cfg.MetricLabelOverrides["tool_success_rate"] = "Tool Success Rate"
	cfg.MetricLabelOverrides["composer_lines_added"] = "Lines Added"
	cfg.MetricLabelOverrides["composer_lines_removed"] = "Lines Removed"
	cfg.MetricLabelOverrides["composer_files_changed"] = "Files Changed"
	cfg.MetricLabelOverrides["scored_commits"] = "Commits"
	cfg.MetricLabelOverrides["total_prompts"] = "Prompts"
	cfg.MetricLabelOverrides["ai_code_percentage"] = "AI Code"
	return cfg
}
