package codex

import "github.com/janekbaraniewski/openusage/internal/core"

func dashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleGreen
	cfg.ShowClientComposition = true
	cfg.GaugePriority = []string{
		"rate_limit_primary", "rate_limit_secondary", "rate_limit_code_review_primary", "context_window",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Session", Keys: []string{"session_input_tokens", "session_output_tokens", "session_cached_tokens", "session_reasoning_tokens"}, MaxSegments: 4},
		{Label: "Limits", Keys: []string{"rate_limit_primary", "rate_limit_secondary", "context_window"}, MaxSegments: 3},
	}
	cfg.HideMetricPrefixes = []string{"client_"}
	cfg.RawGroups = append(cfg.RawGroups,
		core.DashboardRawGroup{
			Label: "Usage Split",
			Keys:  []string{"model_usage", "client_usage"},
		},
	)
	cfg.CompactMetricLabelOverrides["session_input_tokens"] = "in"
	cfg.CompactMetricLabelOverrides["session_output_tokens"] = "out"
	cfg.CompactMetricLabelOverrides["session_cached_tokens"] = "cached"
	cfg.CompactMetricLabelOverrides["session_reasoning_tokens"] = "reason"
	cfg.CompactMetricLabelOverrides["rate_limit_primary"] = "primary"
	cfg.CompactMetricLabelOverrides["rate_limit_secondary"] = "secondary"
	cfg.MetricLabelOverrides["rate_limit_code_review_primary"] = "Code Review Limit"
	cfg.MetricLabelOverrides["rate_limit_code_review_secondary"] = "Code Review Secondary"
	return cfg
}
