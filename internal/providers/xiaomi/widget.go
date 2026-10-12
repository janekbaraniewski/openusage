package xiaomi

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRolePeach))
	// The wallet gauge fills with lifetime spend against everything paid in;
	// the Token Plan gauge appears for accounts that have a plan.
	cfg.GaugePriority = []string{"available_balance", "plan_tokens"}
	cfg.GaugeMaxLines = 2

	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Balance", Keys: []string{"available_balance", "cash_balance", "gift_balance"}, MaxSegments: 3},
		{Label: "Tokens", Keys: []string{"total_tokens", "input_tokens", "output_tokens", "cache_tokens"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides = map[string]string{
		"available_balance": "Wallet Spent",
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
