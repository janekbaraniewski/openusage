package deepseek

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleSky
	cfg.APIKeyEnv = "DEEPSEEK_API_KEY"
	cfg.DefaultAccountID = "deepseek"
	return cfg
}

func (p *Provider) DetailWidget() core.DetailWidget {
	return core.DefaultDetailWidget()
}
