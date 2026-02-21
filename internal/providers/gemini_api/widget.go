package gemini_api

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleBlue
	cfg.APIKeyEnv = "GEMINI_API_KEY"
	cfg.DefaultAccountID = "gemini-api"
	return cfg
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}
