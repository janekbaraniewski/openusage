package claude_code

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleGreen
	cfg.GaugePriority = []string{
		"usage_five_hour", "usage_seven_day", "usage_seven_day_sonnet", "usage_seven_day_opus",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Session", Keys: []string{"5h_block_input", "5h_block_output", "5h_block_msgs", "burn_rate"}, MaxSegments: 4},
		{Label: "Credits", Keys: []string{"today_api_cost", "5h_block_cost", "7d_api_cost", "all_time_api_cost"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "7d_messages"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"7d_input_tokens", "7d_output_tokens", "today_cache_read_tokens", "today_cache_create_tokens"}, MaxSegments: 4},
	}
	cfg.HideMetricPrefixes = []string{"tokens_today_", "input_tokens_", "output_tokens_", "model_"}
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys:  []string{"model_usage", "model_usage_window", "model_count"},
		},
	)
	cfg.MetricLabelOverrides["5h_block_cost"] = "5h Block Cost≈"
	cfg.MetricLabelOverrides["5h_block_input"] = "5h Block In"
	cfg.MetricLabelOverrides["5h_block_output"] = "5h Block Out"
	cfg.MetricLabelOverrides["5h_block_msgs"] = "5h Block Msgs"
	cfg.MetricLabelOverrides["today_api_cost"] = "Today Cost≈"
	cfg.MetricLabelOverrides["7d_api_cost"] = "7-Day Cost≈"
	cfg.MetricLabelOverrides["7d_messages"] = "7-Day Messages"
	cfg.MetricLabelOverrides["7d_input_tokens"] = "7-Day Input"
	cfg.MetricLabelOverrides["7d_output_tokens"] = "7-Day Output"
	cfg.MetricLabelOverrides["all_time_api_cost"] = "All-Time Cost≈"
	cfg.MetricLabelOverrides["usage_five_hour"] = "5-Hour Usage"
	cfg.MetricLabelOverrides["usage_seven_day"] = "7-Day Usage"
	cfg.MetricLabelOverrides["usage_seven_day_sonnet"] = "7d Sonnet Usage"
	cfg.MetricLabelOverrides["usage_seven_day_opus"] = "7d Opus Usage"
	cfg.MetricLabelOverrides["usage_seven_day_cowork"] = "7d Cowork Usage"

	cfg.MetricGroupOverrides["today_api_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Today Cost≈", Order: 2}
	cfg.MetricGroupOverrides["7d_api_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "7-Day Cost≈", Order: 2}
	cfg.MetricGroupOverrides["all_time_api_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "All-Time Cost≈", Order: 2}
	cfg.MetricGroupOverrides["usage_five_hour"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "5-Hour Usage", Order: 1}
	cfg.MetricGroupOverrides["usage_seven_day"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "7-Day Usage", Order: 1}
	cfg.CompactMetricLabelOverrides["today_api_cost"] = "today"
	cfg.CompactMetricLabelOverrides["7d_api_cost"] = "7d"
	cfg.CompactMetricLabelOverrides["all_time_api_cost"] = "all"
	cfg.CompactMetricLabelOverrides["5h_block_cost"] = "5h"
	cfg.CompactMetricLabelOverrides["5h_block_input"] = "5h in"
	cfg.CompactMetricLabelOverrides["5h_block_output"] = "5h out"
	cfg.CompactMetricLabelOverrides["5h_block_msgs"] = "5h msgs"
	cfg.CompactMetricLabelOverrides["today_cache_read_tokens"] = "cache read"
	cfg.CompactMetricLabelOverrides["today_cache_create_tokens"] = "cache write"
	cfg.CompactMetricLabelOverrides["7d_input_tokens"] = "7d in"
	cfg.CompactMetricLabelOverrides["7d_output_tokens"] = "7d out"
	cfg.CompactMetricLabelOverrides["7d_messages"] = "7d msgs"
	cfg.CompactMetricLabelOverrides["burn_rate"] = "burn"
	return cfg
}
