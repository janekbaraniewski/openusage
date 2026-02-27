package alibaba_cloud

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	cfg := providerbase.DefaultDashboard(
		providerbase.WithColorRole(core.DashboardColorRolePeach),
		providerbase.WithGaugePriority("available_balance", "credit_balance", "spend_limit", "rpm", "tpm"),
		providerbase.WithGaugeMaxLines(2),
		providerbase.WithCompactRows(
			core.DashboardCompactRow{
				Label:       "Balance",
				Keys:        []string{"available_balance", "credit_balance", "spend_limit"},
				MaxSegments: 3,
			},
			core.DashboardCompactRow{
				Label:       "Spending",
				Keys:        []string{"daily_spend", "monthly_spend"},
				MaxSegments: 2,
			},
			core.DashboardCompactRow{
				Label:       "Rate Limits",
				Keys:        []string{"rpm", "tpm"},
				MaxSegments: 2,
			},
			core.DashboardCompactRow{
				Label:       "Usage",
				Keys:        []string{"tokens_used", "requests_used"},
				MaxSegments: 2,
			},
		),
		providerbase.WithHideMetricPrefixes("model_"),
		providerbase.WithSuppressZeroMetricKeys("requests_used"),
		providerbase.WithRawGroups(core.DashboardRawGroup{
			Label: "Billing Cycle",
			Keys:  []string{"billing_cycle_start", "billing_cycle_end"},
		}),
	)

	// Overwrite label maps entirely — alibaba uses only its own labels, not defaults.
	cfg.MetricLabelOverrides = map[string]string{
		"available_balance": "Available Balance",
		"credit_balance":    "Credit Balance",
		"spend_limit":       "Spend Limit",
		"daily_spend":       "Daily Spend",
		"monthly_spend":     "Monthly Spend",
		"tokens_used":       "Total Tokens Used",
		"requests_used":     "Total Requests Used",
		"rpm":               "Requests/Min",
		"tpm":               "Tokens/Min",
	}
	cfg.CompactMetricLabelOverrides = map[string]string{
		"available_balance": "avail",
		"credit_balance":    "cred",
		"spend_limit":       "limit",
		"daily_spend":       "today",
		"monthly_spend":     "month",
		"tokens_used":       "tokens",
		"requests_used":     "reqs",
		"rpm":               "RPM",
		"tpm":               "TPM",
	}

	return cfg
}
