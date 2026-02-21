package mistral

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	cfg := core.DefaultDashboardWidget()
	cfg.ColorRole = core.DashboardColorRoleFlamingo
	cfg.APIKeyEnv = "MISTRAL_API_KEY"
	cfg.DefaultAccountID = "mistral"
	return cfg
}
