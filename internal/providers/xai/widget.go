package xai

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleMaroon
	cfg.APIKeyEnv = "XAI_API_KEY"
	cfg.DefaultAccountID = "xai"
	return cfg
}
