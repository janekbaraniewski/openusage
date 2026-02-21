package openrouter

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.DisplayStyle = core.DashboardDisplayStyleOpenRouter
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
	cfg.SuppressZeroMetricKeys = []string{
		"usage_daily", "usage_weekly", "usage_monthly",
		"byok_usage", "byok_daily", "byok_weekly", "byok_monthly",
		"today_byok_cost", "7d_byok_cost", "30d_byok_cost",
	}
	cfg.SuppressZeroNonQuotaMetrics = true
	return cfg
}
