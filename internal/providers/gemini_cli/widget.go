package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleBlue
	cfg.ShowClientComposition = true
	cfg.GaugePriority = []string{
		"tokens_today", "7d_tokens", "messages_today", "sessions_today", "tool_calls_today",
		"client_cli_total_tokens", "client_cli_input_tokens",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Usage", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "total_conversations"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"tokens_today", "7d_tokens", "client_cli_input_tokens", "client_cli_total_tokens"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"total_messages", "total_sessions", "total_turns", "total_tool_calls"}, MaxSegments: 4},
	}
	cfg.CompactMetricLabelOverrides["client_cli_input_tokens"] = "cli in"
	cfg.CompactMetricLabelOverrides["client_cli_total_tokens"] = "cli total"
	cfg.CompactMetricLabelOverrides["tokens_today"] = "today tok"
	cfg.CompactMetricLabelOverrides["7d_tokens"] = "7d tok"
	cfg.MetricLabelOverrides["client_cli_input_tokens"] = "CLI Input Tokens"
	cfg.MetricLabelOverrides["client_cli_total_tokens"] = "CLI Total Tokens"
	cfg.MetricLabelOverrides["total_turns"] = "All-Time Turns"
	cfg.MetricLabelOverrides["total_tool_calls"] = "All-Time Tool Calls"
	cfg.MetricLabelOverrides["tokens_today"] = "Today Tokens"
	cfg.MetricLabelOverrides["7d_tokens"] = "7-Day Tokens"
	return cfg
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}
