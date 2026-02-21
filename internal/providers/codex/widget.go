package codex

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleGreen
	cfg.GaugePriority = []string{
		"rate_limit_primary", "rate_limit_secondary", "context_window",
	}
	cfg.CompactRows = []core.DashboardCompactRow{
		{Label: "Session", Keys: []string{"session_input_tokens", "session_output_tokens", "session_cached_tokens", "session_reasoning_tokens"}, MaxSegments: 4},
		{Label: "Limits", Keys: []string{"rate_limit_primary", "rate_limit_secondary", "context_window"}, MaxSegments: 3},
	}
	return cfg
}
