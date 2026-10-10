package command_code

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// dashboardWidget configures the tile: rolling usage windows and credit balance
// as gauges, spend/activity as compact pills, and plan metadata in the panel.
func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRoleTeal),
		providerbase.WithGaugePriority("credit_balance", "usage_five_hour", "usage_seven_day"),
		providerbase.WithGaugeMaxLines(2),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{Label: "Limits", Keys: []string{"usage_five_hour", "usage_seven_day"}, MaxSegments: 2},
			core.DashboardCompactRow{Label: "Credits", Keys: []string{"credit_balance", "extra_credits", "plan_percent_used"}, MaxSegments: 3},
			core.DashboardCompactRow{Label: "Spend", Keys: []string{"monthly_spend", "today_spend", "requests"}, MaxSegments: 3},
		),
		providerbase.WithMetricLabels(map[string]string{
			"usage_five_hour":   "5-Hour Window",
			"usage_seven_day":   "Weekly Window",
			"credit_balance":    "Credit Balance",
			"extra_credits":     "Extra Credits",
			"plan_percent_used": "Plan Used",
			"monthly_spend":     "Billing Cycle Spend",
			"today_spend":       "Today Spend",
			"total_tokens":      "Billing Cycle Tokens",
			"input_tokens":      "Input Tokens",
			"output_tokens":     "Output Tokens",
			"requests":          "Billing Cycle Requests",
			"requests_today":    "Today Requests",
		}),
		providerbase.WithCompactLabels(map[string]string{
			"usage_five_hour":   "5h",
			"usage_seven_day":   "7d",
			"credit_balance":    "bal",
			"extra_credits":     "extra",
			"plan_percent_used": "used",
			"monthly_spend":     "cycle",
			"today_spend":       "today",
			"requests":          "req",
		}),
		providerbase.WithRawGroups(core.DashboardRawGroup{
			Label: "Command Code",
			Keys: []string{
				"plan_name", "plan_id", "subscription_status",
				"billing_cycle_start", "billing_cycle_end", "organization_name",
			},
		}),
		providerbase.WithSuppressZeroMetricKeys("extra_credits", "requests_today"),
	)

	cfg.APIKeyEnv = EnvAPIKey
	cfg.DefaultAccountID = DefaultAccountID
	cfg.DataSpec = core.WidgetDataSpec{
		RequiredMetricKeys: []string{"credit_balance"},
		MetricPrefixes:     []string{"usage_"},
	}

	return cfg
}
