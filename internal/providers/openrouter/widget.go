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
