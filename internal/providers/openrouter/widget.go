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
		"today_cost", "7d_api_cost", "30d_api_cost",
		"today_requests", "today_input_tokens", "today_output_tokens",
		"today_reasoning_tokens", "today_cached_tokens", "today_image_tokens",
		"recent_requests", "burn_rate", "daily_projected", "limit_remaining",
	}
	cfg.GaugeMaxLines = 1
	cfg.CompactRows = nil
	cfg.HideMetricPrefixes = []string{"model_", "provider_"}
	cfg.HideCreditsWhenBalancePresent = true
	cfg.SuppressZeroMetricKeys = []string{
		"usage_daily", "usage_weekly", "usage_monthly",
		"byok_usage", "byok_daily", "byok_weekly", "byok_monthly",
		"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
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
	cfg.MetricLabelOverrides["analytics_requests"] = "Analytics Requests"
	cfg.MetricLabelOverrides["analytics_byok_cost"] = "Analytics BYOK"
	cfg.MetricLabelOverrides["analytics_reasoning_tokens"] = "Analytics Reasoning"
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
	cfg.MetricGroupOverrides["recent_requests"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Recent Requests", Order: 4}
	cfg.MetricGroupOverrides["daily_projected"] = core.DashboardMetricGroupOverride{Group: "Activity", Label: "Daily Projected", Order: 4}
	cfg.CompactMetricLabelOverrides["today_byok_cost"] = "today byok"
	cfg.CompactMetricLabelOverrides["7d_byok_cost"] = "7d byok"
	cfg.CompactMetricLabelOverrides["30d_byok_cost"] = "30d byok"
	cfg.CompactMetricLabelOverrides["analytics_byok_cost"] = "ana byok"
	cfg.CompactMetricLabelOverrides["analytics_requests"] = "ana req"
	cfg.CompactMetricLabelOverrides["analytics_reasoning_tokens"] = "ana reason"
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
				"activity_models", "activity_providers", "keys_total", "keys_active",
			},
		},
	)
	return cfg
}
