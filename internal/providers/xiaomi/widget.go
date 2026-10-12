package xiaomi

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRolePeach))
	// Token Plan quota is the only capped figure; the wallet balance leads
	// for pay-as-you-go accounts, which have no plan.
	cfg.GaugePriority = []string{"plan_tokens", "available_balance"}
	cfg.GaugeMaxLines = 1

	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Balance", Keys: []string{"available_balance", "cash_balance", "gift_balance"}, MaxSegments: 3},
		{Label: "Spend", Keys: []string{"month_spend", "total_spend"}, MaxSegments: 2},
		{Label: "Tokens", Keys: []string{"total_tokens", "input_tokens", "output_tokens", "cache_tokens"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides = map[string]string{
		"available_balance": "Balance",
		"cash_balance":      "Cash",
		"gift_balance":      "Gift",
		"month_spend":       "This Month",
		"total_spend":       "Lifetime Spend",
		"total_tokens":      "Total Tokens",
		"input_tokens":      "Input Tokens",
		"output_tokens":     "Output Tokens",
		"cache_tokens":      "Cached Tokens",
		"plugin_requests":   "Plugin Requests",
		"plan_tokens":       "Token Plan (month)",
	}

	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{Label: "Account", Keys: []string{"currency", "auth_scope", "console_session_source"}},
		core.DashboardRawGroup{Label: "Token Plan", Keys: []string{"plan_name", "plan_expired"}},
	)
	return cfg
}
