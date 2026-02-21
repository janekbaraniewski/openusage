package claude_code

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleTeal
	cfg.GaugePriority = []string{
		"usage_five_hour", "usage_seven_day", "plan_percent_used",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"today_api_cost", "5h_block_cost", "7d_api_cost", "burn_rate"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "7d_messages"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"5h_block_input", "5h_block_output", "7d_input_tokens", "7d_output_tokens"}, MaxSegments: 4},
	}
	return cfg
}
