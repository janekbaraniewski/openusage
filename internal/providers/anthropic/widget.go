package anthropic

import "github.com/janekbaraniewski/openusage/internal/core"

func (p *Provider) DashboardWidget() core.DashboardWidget {
	return core.DefaultDashboardWidget()
}
