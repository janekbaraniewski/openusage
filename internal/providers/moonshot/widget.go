package moonshot

import (
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

func dashboardWidget() core.DashboardWidget {
	// Routed through providerbase.DefaultDashboard so future option
	// additions in providerbase apply to moonshot uniformly with other
	// providers.
	cfg := providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleMauve))

	// One gauge — total spent vs cumulative deposit (high-water-mark of the
	// observed available balance). Cash/voucher breakdown lives in compact
	// rows below; surfacing them as gauges too would be redundant noise
	// (cash + voucher == available by construction).
	//
	// Gauge fill semantics in this codebase = "% used" — see
	// internal/core/metric_semantics.go. Labels below match that.
	cfg.GaugePriority = []string{"available_balance"}
	cfg.GaugeMaxLines = 1

	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Balance", Keys: []string{"available_balance", "cash_balance", "voucher_balance"}, MaxSegments: 4},
		{Label: "Limits", Keys: []string{"rpm", "tpm", "concurrency_max"}, MaxSegments: 4},
		// Activity row is fed by telemetry events tagged provider_id=moonshot
		// (e.g. OpenCode hooks). Empty until events arrive.
		{Label: "Activity", Keys: []string{"messages_today", "tokens_today", "cost_today"}, MaxSegments: 4},
	}

	cfg.MetricLabelOverrides = map[string]string{
		// Detail-panel labels — "Spent" matches the "% used" gauge fill.
		"available_balance": "Spent",
		"cash_balance":      "Cash spent",
		"voucher_balance":   "Voucher spent",
		"rpm":               "Req / min",
		"tpm":               "Tokens / min",
		"concurrency_max":   "Concurrency",
		"total_token_quota": "Token quota",
	}
	cfg.CompactMetricLabelOverrides = map[string]string{
		"available_balance": "spent",
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
