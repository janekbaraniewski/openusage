package tui

import (
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

var (
	widgetSpecsOnce sync.Once
	widgetSpecs     map[string]core.DashboardWidget
)

func dashboardWidget(providerID string) core.DashboardWidget {
	widgetSpecsOnce.Do(func() {
		widgetSpecs = make(map[string]core.DashboardWidget)
		for _, p := range providers.AllProviders() {
			widgetSpecs[p.ID()] = p.DashboardWidget()
		}
	})

	if cfg, ok := widgetSpecs[providerID]; ok {
		return cfg
	}
	return core.DefaultDashboardWidget()
}
