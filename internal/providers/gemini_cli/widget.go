package gemini_cli

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleBlue
	cfg.ResetStyle = core.DashboardResetStyleCompactModelResets
	cfg.ResetCompactThreshold = 4
	return cfg
}
