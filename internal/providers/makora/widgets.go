package makora

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// dashboardWidget defines the tile layout for the Makora provider.
//
// Balance is the primary metric; its gauge uses the monthly spend limit as
// the reference (Limit) so the user sees remaining credits against the
// monthly budget. monthly_spend is the actual credits consumed in the window
// (spend vs limit), and spend_limit is the fixed monthly cap.
func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleSapphire),
		providerbase.WithGaugeMaxLines(2),
		providerbase.WithGaugePriority(
			"credit_balance", "monthly_spend", "spend_limit",
		),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"credit_balance", "monthly_spend", "spend_limit"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Tokens", Keys: []string{"today_input_tokens", "today_output_tokens", "today_cached_tokens", "today_tokens"}, MaxSegments: 4},
			core.DashboardCompactRow{Label: "Activity", Keys: []string{"today_requests", "30d_requests", "30d_models"}, MaxSegments: 4},
		),
		providerbase.WithSectionOrder(
			core.DashboardSectionHeader,
			core.DashboardSectionTopUsageProgress,
			core.DashboardSectionModelBurn,
			core.DashboardSectionClientBurn,
			core.DashboardSectionToolUsage,
			core.DashboardSectionLanguageBurn,
			core.DashboardSectionDailyUsage,
			core.DashboardSectionOtherData,
		),
		providerbase.WithHideMetricPrefixes(
			"model_", "today_", "30d_",
		),
		providerbase.WithSuppressZeroMetricKeys(
			"today_input_tokens", "today_output_tokens", "today_cached_tokens", "today_tokens", "today_requests",
		),
		providerbase.WithRawGroups(
			core.DashboardRawGroup{Label: "Account", Keys: []string{"account_email", "auth_type", "currency", "has_payment_method"}},
			core.DashboardRawGroup{Label: "Plan", Keys: []string{"pay_as_you_go_enabled", "spend_limit"}},
			core.DashboardRawGroup{Label: "Usage", Keys: []string{"usage_interval", "usage_range", "30d_requests", "30d_models", "request_count_total"}},
			core.DashboardRawGroup{Label: "Rate Card", Keys: []string{"rate_card_updated_at", "models_priced"}},
		),
		providerbase.WithMetricLabels(map[string]string{
			"credit_balance":      "Balance",
			"monthly_spend":       "Month spend",
			"spend_limit":         "Monthly limit",
			"today_input_tokens":  "Input tokens",
			"today_output_tokens": "Output tokens",
			"today_cached_tokens": "Cached tokens",
			"today_tokens":        "Total tokens",
			"today_requests":      "Requests today",
			"30d_requests":        "30d requests",
			"30d_models":          "Models (30d)",
			"request_count_total": "Total requests",
			"models_priced":       "Models priced",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"credit_balance": "bal",
			"monthly_spend":  "spent",
			"spend_limit":    "limit",
			"today_tokens":   "tok",
			"30d_models":     "models",
		}),
	)

	cfg.DisplayStyle = core.DashboardDisplayStyleDetailedCredits
	return cfg
}

// detailWidget defines the right-hand detail panel sections. Models and
// Trends are driven by the snapshot's ModelUsage records and DailySeries, so
// per-model tables and daily spend charts work automatically.
func detailWidget() core.DetailWidget {
	return core.DetailWidget{
		Sections: []core.DetailSection{
			{Name: "Usage", Order: 1, Style: core.DetailSectionStyleUsage},
			{Name: "Models", Order: 2, Style: core.DetailSectionStyleModels},
			{Name: "Spending", Order: 3, Style: core.DetailSectionStyleSpending},
			{Name: "Tokens", Order: 4, Style: core.DetailSectionStyleTokens},
			{Name: "Trends", Order: 5, Style: core.DetailSectionStyleTrends},
			{Name: "Activity", Order: 6, Style: core.DetailSectionStyleActivity},
		},
	}
}
