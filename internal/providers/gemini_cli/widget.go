package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ResetStyle = core.DashboardResetStyleGeminiCompact
	cfg.ResetCompactThreshold = 4
	return cfg
}
