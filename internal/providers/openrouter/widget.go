package openrouter

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleRosewater),
		providerbase.WithGaugeMaxLines(1),
		providerbase.WithGaugePriority(
			"credit_balance", "credits", "usage_daily", "usage_weekly", "usage_monthly",
			"today_cost", "7d_api_cost", "30d_api_cost",
			"today_requests", "today_input_tokens", "today_output_tokens",
			"analytics_7d_cost", "analytics_30d_cost",
			"analytics_7d_requests", "analytics_30d_requests",
			"analytics_7d_tokens", "analytics_30d_tokens",
			"limit_remaining", "burn_rate", "daily_projected",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"credit_balance", "usage_daily", "usage_weekly", "usage_monthly", "limit_remaining"}, MaxSegments: 5},
			core.DashboardCompactRow{Label: "Spend", Keys: []string{"today_cost", "7d_api_cost", "30d_api_cost", "today_byok_cost", "7d_byok_cost", "30d_byok_cost"}, MaxSegments: 5},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"today_requests", "analytics_7d_requests", "analytics_30d_requests", "recent_requests", "keys_active", "keys_disabled"}, MaxSegments: 6},
			core.DashboardCompactRow{Label: "Tokens", Keys: []string{"today_input_tokens", "today_output_tokens", "today_reasoning_tokens", "today_cached_tokens", "analytics_7d_tokens"}, MaxSegments: 5},
			core.DashboardCompactRow{Label: "Perf", Keys: []string{"today_avg_latency", "today_avg_generation_time", "today_avg_moderation_latency", "today_streamed_percent", "burn_rate"}, MaxSegments: 5},
		),
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionModelBurn,
			core.DashboardSectionClientBurn,
			core.DashboardSectionUpstreamProviders,
			core.DashboardSectionActualToolUsage,
			core.DashboardSectionLanguageBurn,
			core.DashboardSectionDailyUsage,
			core.DashboardSectionOtherData,
		),
		providerbase.WithHideMetricPrefixes(
			"model_", "client_", "lang_", "tool_", "provider_", "endpoint_", "analytics_", "keys_", "today_", "7d_", "30d_", "byok_", "usage_", "upstream_",
		),
		providerbase.WithHideMetricKeys(
			"model_usage_unit",
		),
		providerbase.WithSuppressZeroMetricKeys(
			"usage_daily", "usage_weekly", "usage_monthly",
			"byok_usage", "byok_daily", "byok_weekly", "byok_monthly",
			"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
			"analytics_7d_byok_cost", "analytics_30d_byok_cost",
			"today_streamed_percent",
		),
		providerbase.WithRawGroups(
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
		),
		providerbase.WithMetricLabels(map[string]string{
			"usage_daily":                    "Today Usage",
			"usage_weekly":                   "This Week",
			"usage_monthly":                  "This Month",
			"byok_usage":                     "BYOK Total",
			"byok_daily":                     "BYOK Today",
			"byok_weekly":                    "BYOK This Week",
			"byok_monthly":                   "BYOK This Month",
			"7d_byok_cost":                   "7-Day BYOK Cost",
			"30d_byok_cost":                  "30-Day BYOK Cost",
			"today_byok_cost":                "Today BYOK Cost",
			"today_reasoning_tokens":         "Today Reasoning",
			"today_cached_tokens":            "Today Cached",
			"today_image_tokens":             "Today Image Tokens",
			"today_media_prompts":            "Media Prompts",
			"today_audio_inputs":             "Audio Inputs",
			"today_search_results":           "Search Results",
			"today_media_completions":        "Media Completions",
			"today_cancelled":                "Cancelled Requests",
			"today_native_input_tokens":      "Today Native Input",
			"today_native_output_tokens":     "Today Native Output",
			"today_streamed_requests":        "Today Streamed",
			"today_streamed_percent":         "Streamed Share",
			"today_avg_generation_time":      "Avg Generation Time",
			"today_avg_moderation_latency":   "Avg Moderation Time",
			"analytics_7d_cost":              "7-Day Analytics Cost",
			"analytics_30d_cost":             "30-Day Analytics Cost",
			"analytics_7d_byok_cost":         "7-Day Analytics BYOK",
			"analytics_30d_byok_cost":        "30-Day Analytics BYOK",
			"analytics_7d_requests":          "7-Day Analytics Requests",
			"analytics_30d_requests":         "30-Day Analytics Requests",
			"analytics_7d_tokens":            "7-Day Analytics Tokens",
			"analytics_30d_tokens":           "30-Day Analytics Tokens",
			"analytics_7d_input_tokens":      "7-Day Analytics Input",
			"analytics_30d_input_tokens":     "30-Day Analytics Input",
			"analytics_7d_output_tokens":     "7-Day Analytics Output",
			"analytics_30d_output_tokens":    "30-Day Analytics Output",
			"analytics_7d_reasoning_tokens":  "7-Day Analytics Reasoning",
			"analytics_30d_reasoning_tokens": "30-Day Analytics Reasoning",
			"analytics_active_days":          "Analytics Active Days",
			"analytics_models":               "Analytics Models",
			"analytics_providers":            "Analytics Providers",
			"analytics_endpoints":            "Analytics Endpoints",
			"keys_total":                     "Keys Total",
			"keys_active":                    "Keys Active",
			"keys_disabled":                  "Keys Disabled",
			"30d_api_cost":                   "30-Day Cost\u2248",
			"daily_projected":                "Daily Projected",
			"limit_remaining":                "Limit Remaining",
			"recent_requests":                "Recent Requests",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"today_cost":                   "today",
			"7d_api_cost":                  "7d",
			"30d_api_cost":                 "30d",
			"today_byok_cost":              "today byok",
			"7d_byok_cost":                 "7d byok",
			"30d_byok_cost":                "30d byok",
			"analytics_7d_cost":            "ana 7d",
			"analytics_30d_cost":           "ana 30d",
			"analytics_7d_byok_cost":       "ana 7d byok",
			"analytics_30d_byok_cost":      "ana 30d byok",
			"analytics_7d_requests":        "ana 7d req",
			"analytics_30d_requests":       "ana 30d req",
			"analytics_7d_tokens":          "ana 7d tok",
			"today_streamed_percent":       "streamed",
			"today_avg_latency":            "lat",
			"today_avg_generation_time":    "gen",
			"today_avg_moderation_latency": "mod",
			"keys_active":                  "keys",
			"keys_disabled":                "disabled",
		}),
	)

	// Fields without dedicated option helpers.
	cfg.DisplayStyle = core.DashboardDisplayStyleDetailedCredits
	cfg.ShowClientComposition = true
	cfg.ClientCompositionHeading = "Projects"
	cfg.ShowToolComposition = false
	cfg.ShowActualToolUsage = true
	cfg.ShowLanguageComposition = true
	cfg.HideCreditsWhenBalancePresent = true
	cfg.SuppressZeroNonUsageMetrics = true

	return cfg
}
