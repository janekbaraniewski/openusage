package alibaba_cloud

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()

	cfg.ColorRole = core.DashboardColorRolePeach

	// Gauge priority — show most critical metrics as gauge bars
	cfg.GaugePriority = []string{
		"available_balance", "credit_balance", "spend_limit", "rpm", "tpm",
	}
	cfg.GaugeMaxLines = 2

	// Compact rows — summary pills shown in the tile
	cfg.CompactRows = []core.DashboardCompactRow{
		{
			Label:        "Balance",
			Keys:         []string{"available_balance", "credit_balance", "spend_limit"},
			MaxSegments:  3,
		},
		{
			Label:        "Spending",
			Keys:         []string{"daily_spend", "monthly_spend"},
			MaxSegments:  2,
		},
		{
			Label:        "Rate Limits",
			Keys:         []string{"rpm", "tpm"},
			MaxSegments:  2,
		},
		{
			Label:        "Usage",
			Keys:         []string{"tokens_used", "requests_used"},
			MaxSegments:  2,
		},
	}

	// Metric label overrides for detail panel
	cfg.MetricLabelOverrides = map[string]string{
		"available_balance":     "Available Balance",
		"credit_balance":        "Credit Balance",
		"spend_limit":           "Spend Limit",
		"daily_spend":           "Daily Spend",
		"monthly_spend":         "Monthly Spend",
		"tokens_used":           "Total Tokens Used",
		"requests_used":         "Total Requests Used",
		"rpm":                   "Requests/Min",
		"tpm":                   "Tokens/Min",
	}

	// Compact label overrides for tile pills (keep short)
	cfg.CompactMetricLabelOverrides = map[string]string{
		"available_balance":     "avail",
		"credit_balance":        "cred",
		"spend_limit":           "limit",
		"daily_spend":           "today",
		"monthly_spend":         "month",
		"tokens_used":           "tokens",
		"requests_used":         "reqs",
		"rpm":                   "RPM",
		"tpm":                   "TPM",
	}

	// Hide per-model metrics from main tile (too verbose)
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")

	// Suppress metrics that are likely zero
	cfg.SuppressZeroMetricKeys = []string{
		"requests_used",
	}

	// Raw groups — metadata sections in detail panel
	cfg.RawGroups = append(cfg.RawGroups, core.DashboardRawGroup{
		Label: "Billing Cycle",
		Keys:  []string{"billing_cycle_start", "billing_cycle_end"},
	})

	return cfg
}
