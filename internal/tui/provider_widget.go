package tui

import (
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

var (
	providerSpecsOnce sync.Once
	providerWidgets   map[string]core.DashboardWidget
	providerDetails   map[string]core.DetailWidget
	providerOrder     []string
)

func loadProviderSpecs() {
	providerSpecsOnce.Do(func() {
		providerWidgets = make(map[string]core.DashboardWidget)
		providerDetails = make(map[string]core.DetailWidget)
		for _, p := range providers.AllProviders() {
			id := p.ID()
			providerWidgets[id] = p.DashboardWidget()
			providerDetails[id] = p.DetailWidget()
			providerOrder = append(providerOrder, id)
		}
	})
}

func dashboardWidget(providerID string) core.DashboardWidget {
	loadProviderSpecs()

	if cfg, ok := providerWidgets[providerID]; ok {
		return cfg
	}
	return core.DefaultDashboardWidget()
}

func detailWidget(providerID string) core.DetailWidget {
	loadProviderSpecs()

	if cfg, ok := providerDetails[providerID]; ok {
		return cfg
	}
	return core.DefaultDetailWidget()
}

type apiKeyProviderEntry struct {
	ProviderID string
	AccountID  string
	EnvVar     string
}

func apiKeyProviderEntries() []apiKeyProviderEntry {
	loadProviderSpecs()

	var entries []apiKeyProviderEntry
	for _, id := range providerOrder {
		widget := dashboardWidget(id)
		if widget.APIKeyEnv == "" {
			continue
		}
		accountID := widget.DefaultAccountID
		if accountID == "" {
			accountID = id
		}
		entries = append(entries, apiKeyProviderEntry{
			ProviderID: id,
			AccountID:  accountID,
			EnvVar:     widget.APIKeyEnv,
		})
	}
	return entries
}

func isAPIKeyProvider(providerID string) bool {
	return dashboardWidget(providerID).APIKeyEnv != ""
}

func envVarForProvider(providerID string) string {
	return dashboardWidget(providerID).APIKeyEnv
}
