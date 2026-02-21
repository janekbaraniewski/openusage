package openrouter

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleRosewater
	cfg.APIKeyEnv = "OPENROUTER_API_KEY"
	cfg.DefaultAccountID = "openrouter"
	cfg.DisplayStyle = core.DashboardDisplayStyleDetailedCredits
	cfg.GaugePriority = []string{
		"credit_balance", "credits",
		"usage_daily", "usage_weekly", "usage_monthly",
		"byok_daily", "byok_weekly", "byok_monthly",
		"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
		"analytics_7d_byok_cost", "analytics_30d_byok_cost",
		"today_cost", "7d_api_cost", "30d_api_cost",
		"analytics_7d_cost", "analytics_30d_cost",
		"today_requests", "today_input_tokens", "today_output_tokens",
		"today_reasoning_tokens", "today_cached_tokens", "today_image_tokens", "today_native_input_tokens", "today_native_output_tokens",
		"recent_requests", "burn_rate", "daily_projected", "limit_remaining",
	}
	cfg.GaugeMaxLines = 1
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"credit_balance", "usage_daily", "usage_weekly", "usage_monthly", "limit_remaining"}, MaxSegments: 5},
		{Label: "Spend", Keys: []string{"today_cost", "7d_api_cost", "30d_api_cost", "today_byok_cost", "7d_byok_cost", "30d_byok_cost"}, MaxSegments: 5},
		{Label: "Activity", Keys: []string{"today_requests", "analytics_7d_requests", "analytics_30d_requests", "recent_requests", "keys_active", "keys_disabled"}, MaxSegments: 5},
		{Label: "Tokens", Keys: []string{"today_input_tokens", "today_output_tokens", "today_reasoning_tokens", "today_cached_tokens", "analytics_7d_tokens"}, MaxSegments: 5},
		{Label: "Perf", Keys: []string{"today_avg_latency", "today_avg_generation_time", "today_avg_moderation_latency", "today_streamed_percent", "burn_rate"}, MaxSegments: 5},
	}
	cfg.HideMetricPrefixes = []string{"model_", "provider_", "endpoint_", "analytics_", "keys_", "today_", "7d_", "30d_", "byok_", "usage_"}
	cfg.HideCreditsWhenBalancePresent = true
	cfg.SuppressZeroMetricKeys = []string{
		"usage_daily", "usage_weekly", "usage_monthly",
		"byok_usage", "byok_daily", "byok_weekly", "byok_monthly",
		"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
		"analytics_7d_byok_cost", "analytics_30d_byok_cost",
		"today_streamed_percent",
	}
	cfg.SuppressZeroNonQuotaMetrics = true
	cfg.MetricLabelOverrides["usage_daily"] = "Today Usage"
	cfg.MetricLabelOverrides["usage_weekly"] = "This Week"
	cfg.MetricLabelOverrides["usage_monthly"] = "This Month"
	cfg.MetricLabelOverrides["byok_usage"] = "BYOK Total"
	cfg.MetricLabelOverrides["byok_daily"] = "BYOK Today"
	cfg.MetricLabelOverrides["byok_weekly"] = "BYOK This Week"
	cfg.MetricLabelOverrides["byok_monthly"] = "BYOK This Month"
	cfg.MetricLabelOverrides["7d_byok_cost"] = "7-Day BYOK Cost"
	cfg.MetricLabelOverrides["30d_byok_cost"] = "30-Day BYOK Cost"
	cfg.MetricLabelOverrides["today_byok_cost"] = "Today BYOK Cost"
	cfg.MetricLabelOverrides["today_reasoning_tokens"] = "Today Reasoning"
	cfg.MetricLabelOverrides["today_cached_tokens"] = "Today Cached"
	cfg.MetricLabelOverrides["today_image_tokens"] = "Today Image Tokens"
	cfg.MetricLabelOverrides["today_media_prompts"] = "Media Prompts"
	cfg.MetricLabelOverrides["today_audio_inputs"] = "Audio Inputs"
	cfg.MetricLabelOverrides["today_search_results"] = "Search Results"
	cfg.MetricLabelOverrides["today_media_completions"] = "Media Completions"
	cfg.MetricLabelOverrides["today_cancelled"] = "Cancelled Requests"
	cfg.MetricLabelOverrides["today_native_input_tokens"] = "Today Native Input"
	cfg.MetricLabelOverrides["today_native_output_tokens"] = "Today Native Output"
	cfg.MetricLabelOverrides["today_streamed_requests"] = "Today Streamed"
	cfg.MetricLabelOverrides["today_streamed_percent"] = "Streamed Share"
	cfg.MetricLabelOverrides["today_avg_generation_time"] = "Avg Generation Time"
	cfg.MetricLabelOverrides["today_avg_moderation_latency"] = "Avg Moderation Time"
	cfg.MetricLabelOverrides["analytics_7d_cost"] = "7-Day Analytics Cost"
	cfg.MetricLabelOverrides["analytics_30d_cost"] = "30-Day Analytics Cost"
	cfg.MetricLabelOverrides["analytics_7d_byok_cost"] = "7-Day Analytics BYOK"
	cfg.MetricLabelOverrides["analytics_30d_byok_cost"] = "30-Day Analytics BYOK"
	cfg.MetricLabelOverrides["analytics_7d_requests"] = "7-Day Analytics Requests"
	cfg.MetricLabelOverrides["analytics_30d_requests"] = "30-Day Analytics Requests"
	cfg.MetricLabelOverrides["analytics_7d_tokens"] = "7-Day Analytics Tokens"
	cfg.MetricLabelOverrides["analytics_30d_tokens"] = "30-Day Analytics Tokens"
	cfg.MetricLabelOverrides["analytics_7d_input_tokens"] = "7-Day Analytics Input"
	cfg.MetricLabelOverrides["analytics_30d_input_tokens"] = "30-Day Analytics Input"
	cfg.MetricLabelOverrides["analytics_7d_output_tokens"] = "7-Day Analytics Output"
	cfg.MetricLabelOverrides["analytics_30d_output_tokens"] = "30-Day Analytics Output"
	cfg.MetricLabelOverrides["analytics_7d_reasoning_tokens"] = "7-Day Analytics Reasoning"
	cfg.MetricLabelOverrides["analytics_30d_reasoning_tokens"] = "30-Day Analytics Reasoning"
	cfg.MetricLabelOverrides["analytics_active_days"] = "Analytics Active Days"
	cfg.MetricLabelOverrides["analytics_models"] = "Analytics Models"
	cfg.MetricLabelOverrides["analytics_providers"] = "Analytics Providers"
	cfg.MetricLabelOverrides["analytics_endpoints"] = "Analytics Endpoints"
	cfg.MetricLabelOverrides["keys_total"] = "Keys Total"
	cfg.MetricLabelOverrides["keys_active"] = "Keys Active"
	cfg.MetricLabelOverrides["keys_disabled"] = "Keys Disabled"
	cfg.MetricLabelOverrides["30d_api_cost"] = "30-Day Cost≈"
	cfg.MetricLabelOverrides["daily_projected"] = "Daily Projected"
	cfg.MetricLabelOverrides["limit_remaining"] = "Limit Remaining"
	cfg.MetricLabelOverrides["recent_requests"] = "Recent Requests"

	cfg.MetricGroupOverrides["usage_daily"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "Today Usage", Order: 1}
	cfg.MetricGroupOverrides["usage_weekly"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "This Week", Order: 1}
	cfg.MetricGroupOverrides["usage_monthly"] = core.DashboardMetricGroupOverride{Group: "Usage", Label: "This Month", Order: 1}
	cfg.MetricGroupOverrides["byok_usage"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "BYOK Total", Order: 2}
	cfg.MetricGroupOverrides["byok_daily"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "BYOK Today", Order: 2}
	cfg.MetricGroupOverrides["byok_weekly"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "BYOK This Week", Order: 2}
	cfg.MetricGroupOverrides["byok_monthly"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "BYOK This Month", Order: 2}
	cfg.MetricGroupOverrides["today_byok_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Today BYOK Cost", Order: 2}
	cfg.MetricGroupOverrides["7d_byok_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "7-Day BYOK Cost", Order: 2}
	cfg.MetricGroupOverrides["30d_byok_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "30-Day BYOK Cost", Order: 2}
	cfg.MetricGroupOverrides["analytics_7d_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "7-Day Analytics Cost", Order: 2}
	cfg.MetricGroupOverrides["analytics_30d_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "30-Day Analytics Cost", Order: 2}
	cfg.MetricGroupOverrides["analytics_7d_byok_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "7-Day Analytics BYOK", Order: 2}
	cfg.MetricGroupOverrides["analytics_30d_byok_cost"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "30-Day Analytics BYOK", Order: 2}
	cfg.MetricGroupOverrides["burn_rate"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Burn Rate", Order: 2}
	cfg.MetricGroupOverrides["daily_projected"] = core.DashboardMetricGroupOverride{Group: "Spending", Label: "Daily Projected", Order: 2}
	cfg.MetricGroupOverrides["today_native_input_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "Today Native Input", Order: 3}
	cfg.MetricGroupOverrides["today_native_output_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "Today Native Output", Order: 3}
	cfg.MetricGroupOverrides["analytics_7d_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "7-Day Analytics Tokens", Order: 3}
	cfg.MetricGroupOverrides["analytics_30d_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "30-Day Analytics Tokens", Order: 3}
	cfg.MetricGroupOverrides["analytics_7d_input_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "7-Day Analytics Input", Order: 3}
	cfg.MetricGroupOverrides["analytics_30d_input_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "30-Day Analytics Input", Order: 3}
	cfg.MetricGroupOverrides["analytics_7d_output_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "7-Day Analytics Output", Order: 3}
	cfg.MetricGroupOverrides["analytics_30d_output_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "30-Day Analytics Output", Order: 3}
	cfg.MetricGroupOverrides["analytics_7d_reasoning_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "7-Day Analytics Reasoning", Order: 3}
	cfg.MetricGroupOverrides["analytics_30d_reasoning_tokens"] = core.DashboardMetricGroupOverride{Group: "Tokens", Label: "30-Day Analytics Reasoning", Order: 3}
	cfg.MetricGroupOverrides["recent_requests"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Recent Requests", Order: 4}
	cfg.MetricGroupOverrides["today_streamed_percent"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Streamed Share", Order: 4}
	cfg.MetricGroupOverrides["today_streamed_requests"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Today Streamed", Order: 4}
	cfg.MetricGroupOverrides["today_avg_generation_time"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Avg Generation Time", Order: 4}
	cfg.MetricGroupOverrides["today_avg_moderation_latency"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Avg Moderation Time", Order: 4}
	cfg.MetricGroupOverrides["analytics_7d_requests"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "7-Day Analytics Requests", Order: 4}
	cfg.MetricGroupOverrides["analytics_30d_requests"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "30-Day Analytics Requests", Order: 4}
	cfg.MetricGroupOverrides["analytics_active_days"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Analytics Active Days", Order: 4}
	cfg.MetricGroupOverrides["analytics_models"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Analytics Models", Order: 4}
	cfg.MetricGroupOverrides["analytics_providers"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Analytics Providers", Order: 4}
	cfg.MetricGroupOverrides["analytics_endpoints"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Analytics Endpoints", Order: 4}
	cfg.MetricGroupOverrides["keys_total"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Keys Total", Order: 4}
	cfg.MetricGroupOverrides["keys_active"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Keys Active", Order: 4}
	cfg.MetricGroupOverrides["keys_disabled"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Keys Disabled", Order: 4}

	cfg.CompactMetricLabelOverrides["today_cost"] = "today"
	cfg.CompactMetricLabelOverrides["7d_api_cost"] = "7d"
	cfg.CompactMetricLabelOverrides["30d_api_cost"] = "30d"
	cfg.CompactMetricLabelOverrides["today_byok_cost"] = "today byok"
	cfg.CompactMetricLabelOverrides["7d_byok_cost"] = "7d byok"
	cfg.CompactMetricLabelOverrides["30d_byok_cost"] = "30d byok"
	cfg.CompactMetricLabelOverrides["analytics_7d_cost"] = "ana 7d"
	cfg.CompactMetricLabelOverrides["analytics_30d_cost"] = "ana 30d"
	cfg.CompactMetricLabelOverrides["analytics_7d_byok_cost"] = "ana 7d byok"
	cfg.CompactMetricLabelOverrides["analytics_30d_byok_cost"] = "ana 30d byok"
	cfg.CompactMetricLabelOverrides["analytics_7d_requests"] = "ana 7d req"
	cfg.CompactMetricLabelOverrides["analytics_30d_requests"] = "ana 30d req"
	cfg.CompactMetricLabelOverrides["analytics_7d_tokens"] = "ana 7d tok"
	cfg.CompactMetricLabelOverrides["today_streamed_percent"] = "streamed"
	cfg.CompactMetricLabelOverrides["today_avg_latency"] = "lat"
	cfg.CompactMetricLabelOverrides["today_avg_generation_time"] = "gen"
	cfg.CompactMetricLabelOverrides["today_avg_moderation_latency"] = "mod"
	cfg.CompactMetricLabelOverrides["keys_active"] = "keys"
	cfg.CompactMetricLabelOverrides["keys_disabled"] = "disabled"

	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "API Key",
			Keys: []string{
				"key_label", "key_name", "key_type", "key_disabled", "tier", "is_free_tier", "is_management_key", "is_provisioning_key",
				"key_created_at", "key_updated_at", "key_hash_prefix", "key_lookup",
				"expires_at", "limit_reset", "include_byok_in_limit", "rate_limit_note", "byok_in_use",
			},
		},
		core.DashboardRawGroup{
			Label: "Activity",
			Keys: []string{
				"generations_fetched", "activity_endpoint", "activity_rows", "activity_date_range",
				"activity_days", "activity_models", "activity_providers", "activity_endpoints",
				"keys_total", "keys_active", "keys_disabled",
			},
		},
		core.DashboardRawGroup{
			Label: "Generation",
			Keys: []string{
				"generation_note", "today_finish_reasons", "today_origins", "today_routers",
			},
		},
	)
	return cfg
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}
