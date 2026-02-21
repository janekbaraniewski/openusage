package openai

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleGreen
	cfg.APIKeyEnv = "OPENAI_API_KEY"
	cfg.DefaultAccountID = "openai"
	return cfg
}
