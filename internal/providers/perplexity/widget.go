package perplexity

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	// Routed through providerbase.DefaultDashboard so future option
	// additions in providerbase apply to perplexity uniformly with other
	// providers.
	cfg := providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleSky))
	// Single primary gauge — balance (USD remaining). Tier is shown as a
	// raw line below since tier 0/5 doesn't make sense as a percent.
	cfg.GaugePriority = []string{"available_balance"}
	cfg.GaugeMaxLines = 1

	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Balance", Keys: []string{"available_balance", "pending_balance", "total_spend"}, MaxSegments: 4},
		{Label: "Tier", Keys: []string{"usage_tier", "auto_top_up_amount", "auto_top_up_threshold"}, MaxSegments: 4},
		{Label: "Activity", Keys: []string{"requests_window", "input_tokens_window", "output_tokens_window", "search_queries_window"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides = map[string]string{
		"available_balance":       "Balance",
		"pending_balance":         "Pending",
		"total_spend":             "Lifetime Spend",
		"usage_tier":              "Tier",
		"auto_top_up_amount":      "Auto-reload $",
		"auto_top_up_threshold":   "Auto-reload threshold",
		"requests_window":         "Requests (30d)",
		"input_tokens_window":     "Input Tokens (30d)",
		"output_tokens_window":    "Output Tokens (30d)",
		"reasoning_tokens_window": "Reasoning Tokens (30d)",
		"citation_tokens_window":  "Citation Tokens (30d)",
		"search_queries_window":   "Searches (30d)",
		"pro_search_window":       "Pro Searches (30d)",
	}

	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{Label: "Account", Keys: []string{"account_email", "account_country", "is_pro", "auth_scope", "console_session_browser"}},
		core.DashboardRawGroup{Label: "Org", Keys: []string{"org_id", "org_display_name", "usage_tier"}},
		core.DashboardRawGroup{Label: "Payment", Keys: []string{"payment_method_brand", "payment_method_last4"}},
	)
	return cfg
}
