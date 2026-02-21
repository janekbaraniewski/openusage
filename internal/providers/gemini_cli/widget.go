package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleBlue
	cfg.ShowClientComposition = true
	cfg.ShowToolComposition = true
	cfg.GaugeMaxLines = 1
	cfg.GaugePriority = []string{
		"quota", "quota_pro", "quota_flash", "context_window", "tokens_today", "7d_tokens", "messages_today", "sessions_today", "tool_calls_today",
		"client_cli_total_tokens", "client_cli_input_tokens",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Usage", Keys: []string{"quota", "quota_models_exhausted", "quota_models_low", "quota_models_tracked"}, MaxSegments: 4},
		{Label: "Usage", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_conversations"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"tokens_today", "7d_tokens", "today_input_tokens", "today_output_tokens"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"total_messages", "total_sessions", "total_turns", "total_tool_calls"}, MaxSegments: 4},
	}
	cfg.CompactMetricLabelOverrides["client_cli_input_tokens"] = "cli in"
	cfg.CompactMetricLabelOverrides["client_cli_total_tokens"] = "cli total"
	cfg.CompactMetricLabelOverrides["tokens_today"] = "today tok"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	cfg.CompactMetricLabelOverrides["quota"] = "all"
	cfg.CompactMetricLabelOverrides["quota_pro"] = "pro"
	cfg.CompactMetricLabelOverrides["quota_flash"] = "flash"
	cfg.CompactMetricLabelOverrides["today_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["today_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["quota_models_exhausted"] = "exhausted"
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
	return cfg
}
