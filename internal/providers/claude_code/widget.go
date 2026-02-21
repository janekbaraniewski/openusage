package claude_code

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleGreen
	cfg.ShowClientComposition = true
	cfg.ShowToolComposition = true
	cfg.GaugePriority = []string{
		"usage_five_hour", "usage_seven_day", "usage_seven_day_sonnet", "usage_seven_day_opus", "usage_seven_day_cowork",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Session", Keys: []string{"5h_block_input", "5h_block_output", "5h_block_cache_read_tokens", "5h_block_msgs"}, MaxSegments: 4},
		{Label: "Credits", Keys: []string{"today_api_cost", "5h_block_cost", "7d_api_cost", "all_time_api_cost"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"messages_today", "sessions_today", "tool_calls_today", "7d_tool_calls"}, MaxSegments: 4},
		{Label: "Tokens", Keys: []string{"7d_input_tokens", "7d_output_tokens", "7d_cache_read_tokens", "7d_cache_create_tokens"}, MaxSegments: 4},
		{Label: "Context", Keys: []string{"today_cache_create_1h_tokens", "today_cache_create_5m_tokens", "7d_reasoning_tokens", "burn_rate"}, MaxSegments: 4},
		{Label: "Tools", Keys: []string{"today_web_search_requests", "today_web_fetch_requests", "7d_web_search_requests", "7d_web_fetch_requests"}, MaxSegments: 4},
	}
	cfg.HideMetricPrefixes = []string{"tokens_today_", "input_tokens_", "output_tokens_", "model_", "client_"}
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys:  []string{"model_usage", "model_usage_window", "model_count"},
		},
		core.DashboardRawGroup{
			Label: "Context",
			Keys:  []string{"cache_usage", "service_tier_usage", "inference_geo_usage"},
		},
		core.DashboardRawGroup{
			Label: "Workload",
			Keys: []string{
				"tool_usage", "tool_count", "project_usage", "project_count", "agent_usage",
				"jsonl_total_entries", "jsonl_unique_requests", "jsonl_total_blocks", "jsonl_files_found",
			},
		},
		core.DashboardRawGroup{
			Label: "Source",
			Keys:  []string{"stats_path", "stats_candidates", "stats_last_computed"},
		},
	)
	cfg.MetricLabelOverrides["5h_block_cost"] = "Usage 5h Cost≈"
	cfg.MetricLabelOverrides["5h_block_input"] = "Usage 5h In"
	cfg.MetricLabelOverrides["5h_block_output"] = "Usage 5h Out"
	cfg.MetricLabelOverrides["5h_block_cache_read_tokens"] = "Usage 5h Cache Read"
	cfg.MetricLabelOverrides["5h_block_cache_create_tokens"] = "Usage 5h Cache Write"
	cfg.MetricLabelOverrides["5h_block_msgs"] = "Usage 5h Msgs"
	cfg.MetricLabelOverrides["today_api_cost"] = "Today Cost≈"
	cfg.MetricLabelOverrides["7d_api_cost"] = "7-Day Cost≈"
	cfg.MetricLabelOverrides["7d_messages"] = "7-Day Messages"
	cfg.MetricLabelOverrides["7d_sessions"] = "7-Day Sessions"
	cfg.MetricLabelOverrides["7d_tool_calls"] = "7-Day Tool Calls"
	cfg.MetricLabelOverrides["7d_input_tokens"] = "7-Day Input"
	cfg.MetricLabelOverrides["7d_output_tokens"] = "7-Day Output"
	cfg.MetricLabelOverrides["7d_cache_read_tokens"] = "7-Day Cache Read"
	cfg.MetricLabelOverrides["7d_cache_create_tokens"] = "7-Day Cache Write"
	cfg.MetricLabelOverrides["7d_reasoning_tokens"] = "7-Day Reasoning"
	cfg.MetricLabelOverrides["today_cache_create_1h_tokens"] = "Today Cache 1h"
	cfg.MetricLabelOverrides["today_cache_create_5m_tokens"] = "Today Cache 5m"
	cfg.MetricLabelOverrides["today_web_search_requests"] = "Today Web Search"
	cfg.MetricLabelOverrides["today_web_fetch_requests"] = "Today Web Fetch"
	cfg.MetricLabelOverrides["7d_web_search_requests"] = "7-Day Web Search"
	cfg.MetricLabelOverrides["7d_web_fetch_requests"] = "7-Day Web Fetch"
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
	cfg.CompactMetricLabelOverrides["5h_block_cache_read_tokens"] = "5h cached"
	cfg.CompactMetricLabelOverrides["5h_block_cache_create_tokens"] = "5h write"
	cfg.CompactMetricLabelOverrides["5h_block_msgs"] = "5h msgs"
	cfg.CompactMetricLabelOverrides["today_cache_read_tokens"] = "cache read"
	cfg.CompactMetricLabelOverrides["today_cache_create_tokens"] = "cache write"
	cfg.CompactMetricLabelOverrides["today_cache_create_1h_tokens"] = "cache 1h"
	cfg.CompactMetricLabelOverrides["today_cache_create_5m_tokens"] = "cache 5m"
	cfg.CompactMetricLabelOverrides["7d_input_tokens"] = "7d in"
	cfg.CompactMetricLabelOverrides["7d_output_tokens"] = "7d out"
	cfg.CompactMetricLabelOverrides["7d_cache_read_tokens"] = "7d cached"
	cfg.CompactMetricLabelOverrides["7d_cache_create_tokens"] = "7d write"
	cfg.CompactMetricLabelOverrides["7d_reasoning_tokens"] = "7d reason"
	cfg.CompactMetricLabelOverrides["7d_messages"] = "7d msgs"
	cfg.CompactMetricLabelOverrides["7d_tool_calls"] = "7d tools"
	cfg.CompactMetricLabelOverrides["today_web_search_requests"] = "search"
	cfg.CompactMetricLabelOverrides["today_web_fetch_requests"] = "fetch"
	cfg.CompactMetricLabelOverrides["7d_web_search_requests"] = "7d search"
	cfg.CompactMetricLabelOverrides["7d_web_fetch_requests"] = "7d fetch"
	cfg.CompactMetricLabelOverrides["burn_rate"] = "burn"
	return cfg
}
