package anthropic

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRolePeach
	cfg.APIKeyEnv = "ANTHROPIC_API_KEY"
	cfg.DefaultAccountID = "anthropic"
	return cfg
}
