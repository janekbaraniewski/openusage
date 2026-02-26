package claude_code

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
		"usage_five_hour", "usage_seven_day", "usage_seven_day_sonnet", "usage_seven_day_opus", "usage_seven_day_cowork",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"today_api_cost", "5h_block_cost", "7d_api_cost", "all_time_api_cost"}, MaxSegments: 5},
		{Label: "Usage", Keys: []string{"usage_five_hour", "usage_seven_day", "usage_seven_day_sonnet", "usage_seven_day_opus"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "7d_tool_calls"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"today_input_tokens", "today_output_tokens", "7d_input_tokens", "7d_output_tokens"}, MaxSegments: 4},
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
		core.DashboardSectionOtherData,
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes,
		"tokens_today_", "input_tokens_", "output_tokens_", "model_", "client_", "tool_", "provider_",
		"today_", "7d_", "all_time_", "5h_block_", "interface_", "project_", "agent_",
	)
	cfg.HideMetricKeys = append(cfg.HideMetricKeys,
		"all_time_tool_calls", "tool_calls_total", "tool_completed", "tool_errored", "tool_cancelled", "tool_success_rate",
		"all_time_input_tokens", "all_time_output_tokens", "all_time_cache_read_tokens", "all_time_cache_create_tokens",
		"all_time_cache_create_5m_tokens", "all_time_cache_create_1h_tokens", "all_time_reasoning_tokens",
	)
	cfg.MetricLabelOverrides["today_api_cost"] = "Today Cost"
	cfg.MetricLabelOverrides["5h_block_cost"] = "5h Cost"
	cfg.MetricLabelOverrides["7d_api_cost"] = "7-Day Cost"
	cfg.MetricLabelOverrides["all_time_api_cost"] = "All-Time Cost"
	cfg.MetricLabelOverrides["usage_five_hour"] = "5-Hour Usage"
	cfg.MetricLabelOverrides["usage_seven_day"] = "7-Day Usage"
	cfg.MetricLabelOverrides["usage_seven_day_sonnet"] = "7d Sonnet Usage"
	cfg.MetricLabelOverrides["usage_seven_day_opus"] = "7d Opus Usage"
	cfg.MetricLabelOverrides["usage_seven_day_cowork"] = "7d Team Usage"
	cfg.MetricLabelOverrides["composer_lines_added"] = "Lines Added"
	cfg.MetricLabelOverrides["composer_lines_removed"] = "Lines Removed"
	cfg.MetricLabelOverrides["composer_files_changed"] = "Files Changed"
	cfg.MetricLabelOverrides["scored_commits"] = "Commits"
	cfg.MetricLabelOverrides["total_prompts"] = "Prompts"
	cfg.CompactMetricLabelOverrides["today_api_cost"] = "today"
	cfg.CompactMetricLabelOverrides["5h_block_cost"] = "5h"
	cfg.CompactMetricLabelOverrides["7d_api_cost"] = "7d"
	cfg.CompactMetricLabelOverrides["all_time_api_cost"] = "all"
	cfg.CompactMetricLabelOverrides["usage_five_hour"] = "5h"
	cfg.CompactMetricLabelOverrides["usage_seven_day"] = "7d"
	cfg.CompactMetricLabelOverrides["usage_seven_day_sonnet"] = "sonnet"
	cfg.CompactMetricLabelOverrides["usage_seven_day_opus"] = "opus"
	cfg.CompactMetricLabelOverrides["messages_today"] = "msgs"
	cfg.CompactMetricLabelOverrides["sessions_today"] = "sess"
	cfg.CompactMetricLabelOverrides["tool_calls_today"] = "tools"
	cfg.CompactMetricLabelOverrides["7d_tool_calls"] = "7d tools"
	cfg.CompactMetricLabelOverrides["today_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["today_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["7d_input_tokens"] = "7d in"
	cfg.CompactMetricLabelOverrides["7d_output_tokens"] = "7d out"
	cfg.CompactMetricLabelOverrides["composer_lines_added"] = "added"
	cfg.CompactMetricLabelOverrides["composer_lines_removed"] = "removed"
	cfg.CompactMetricLabelOverrides["composer_files_changed"] = "files"
	cfg.CompactMetricLabelOverrides["scored_commits"] = "commits"
	cfg.CompactMetricLabelOverrides["total_prompts"] = "prompts"
	return cfg
}
