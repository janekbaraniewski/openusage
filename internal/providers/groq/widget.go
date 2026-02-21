package groq

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleYellow
	cfg.APIKeyEnv = "GROQ_API_KEY"
	cfg.DefaultAccountID = "groq"
	return cfg
}
