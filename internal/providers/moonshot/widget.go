package moonshot

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleMauve

	// Moonshot doesn't return per-request Remaining for rate limits, so gauges
	// surface the balance breakdown — which has both a meaningful value and
	// an implicit cap (the cumulative deposit).
	cfg.GaugePriority = []string{
		"available_balance", "cash_balance", "voucher_balance",
	}
	cfg.GaugeMaxLines = 2

	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Balance", Keys: []string{"available_balance", "cash_balance", "voucher_balance"}, MaxSegments: 4},
		{Label: "Limits", Keys: []string{"rpm", "tpm", "concurrency_max"}, MaxSegments: 4},
		// Activity row is fed by telemetry events tagged provider_id=moonshot
		// (e.g. OpenCode hooks). Empty until events arrive.
		{Label: "Activity", Keys: []string{"messages_today", "tokens_today", "cost_today"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides = map[string]string{
		"available_balance": "Available",
		"cash_balance":      "Cash",
		"voucher_balance":   "Vouchers",
		"rpm":               "Req / min",
		"tpm":               "Tokens / min",
		"concurrency_max":   "Concurrency",
		"total_token_quota": "Token Quota",
	}
	cfg.CompactMetricLabelOverrides = map[string]string{
		"available_balance": "avail",
		"cash_balance":      "cash",
		"voucher_balance":   "vouch",
		"concurrency_max":   "conc",
		"total_token_quota": "tquota",
	}
	cfg.HideMetricPrefixes = append(cfg.HideMetricPrefixes, "model_")

	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{Label: "Account", Keys: []string{"account_tier", "service_region", "currency", "user_state"}},
		core.DashboardRawGroup{Label: "Org", Keys: []string{"org_id", "project_id", "access_key_suffix"}},
	)
	return cfg
}
