package openrouter

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleRosewater
	cfg.DisplayStyle = core.DashboardDisplayStyleDetailedCredits
	cfg.ShowClientComposition = true
	cfg.ClientCompositionHeading = "Projects"
	cfg.ShowToolComposition = false
	cfg.ShowActualToolUsage = true
	cfg.ShowLanguageComposition = true
	cfg.GaugePriority = []string{
		"credit_balance", "credits", "usage_daily", "usage_weekly", "usage_monthly",
		"today_cost", "7d_api_cost", "30d_api_cost",
		"today_requests", "today_input_tokens", "today_output_tokens",
		"analytics_7d_cost", "analytics_30d_cost",
		"analytics_7d_requests", "analytics_30d_requests",
		"analytics_7d_tokens", "analytics_30d_tokens",
		"limit_remaining", "burn_rate", "daily_projected",
	}
	cfg.GaugeMaxLines = 1
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Credits", Keys: []string{"credit_balance", "usage_daily", "usage_weekly", "usage_monthly", "limit_remaining"}, MaxSegments: 5},
		{Label: "Spend", Keys: []string{"today_cost", "7d_api_cost", "30d_api_cost", "today_byok_cost", "7d_byok_cost", "30d_byok_cost"}, MaxSegments: 5},
		{Label: "Activity", Keys: []string{"today_requests", "analytics_7d_requests", "analytics_30d_requests", "recent_requests", "keys_active", "keys_disabled"}, MaxSegments: 6},
		{Label: "Tokens", Keys: []string{"today_input_tokens", "today_output_tokens", "today_reasoning_tokens", "today_cached_tokens", "analytics_7d_tokens"}, MaxSegments: 5},
		{Label: "Perf", Keys: []string{"today_avg_latency", "today_avg_generation_time", "today_avg_moderation_latency", "today_streamed_percent", "burn_rate"}, MaxSegments: 5},
	}
	cfg.StandardSectionOrder = []core.DashboardStandardSection{
		core.DashboardSectionHeader,
		core.DashboardSectionTopUsageProgress,
		core.DashboardSectionModelBurn,
		core.DashboardSectionClientBurn,
		core.DashboardSectionUpstreamProviders,
		core.DashboardSectionActualToolUsage,
		core.DashboardSectionLanguageBurn,
		core.DashboardSectionDailyUsage,
		core.DashboardSectionOtherData,
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes,
		"model_", "client_", "lang_", "tool_", "provider_", "endpoint_", "analytics_", "keys_", "today_", "7d_", "30d_", "byok_", "usage_", "upstream_",
	)
	cfg.HideCreditsWhenBalancePresent = true
	cfg.SuppressZeroMetricKeys = []string{
		"usage_daily", "usage_weekly", "usage_monthly",
		"byok_usage", "byok_daily", "byok_weekly", "byok_monthly",
		"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
		"analytics_7d_byok_cost", "analytics_30d_byok_cost",
		"today_streamed_percent",
	}
	cfg.SuppressZeroNonUsageMetrics = true
	cfg.HideMetricKeys = append(cfg.HideMetricKeys,
		"model_usage_unit",
	)
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
			Label: "Usage Split",
			Keys: []string{
				"model_usage", "model_usage_window", "client_usage", "tool_usage", "tool_usage_source", "language_usage", "language_usage_source", "model_mix_source",
			},
		},
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
				"generation_note", "today_finish_reasons", "today_origins", "today_routers", "generations_fetched",
				"generation_provider_detail_lookups", "generation_provider_detail_hits", "provider_resolution",
			},
		},
	)
	return cfg
}
