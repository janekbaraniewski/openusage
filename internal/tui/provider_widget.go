package tui

import (
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

var (
	providerSpecsOnce sync.Once
	providerSpecs     map[string]core.ProviderSpec
	providerWidgets   map[string]core.DashboardWidget
	providerDetails   map[string]core.DetailWidget
	providerOrder     []string
)

func loadProviderSpecs() {
	providerSpecsOnce.Do(func() {
		providerSpecs = make(map[string]core.ProviderSpec)
		providerWidgets = make(map[string]core.DashboardWidget)
		providerDetails = make(map[string]core.DetailWidget)
		for _, p := range providers.AllProviders() {
			spec := p.Spec()
			id := spec.ID
			if id == "" {
				id = p.ID()
			}
			providerSpecs[id] = spec
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
		spec := providerSpecs[id]
		if spec.Auth.Type != core.ProviderAuthTypeAPIKey {
			continue
		}
		widget := dashboardWidget(id)
		envVar := spec.Auth.APIKeyEnv
		if envVar == "" {
			envVar = widget.APIKeyEnv
		}
		if envVar == "" {
			continue
		}
		accountID := spec.Auth.DefaultAccountID
		if accountID == "" {
			accountID = widget.DefaultAccountID
		}
		if accountID == "" {
			accountID = id
		}
		entries = append(entries, apiKeyProviderEntry{
			ProviderID: id,
			AccountID:  accountID,
			EnvVar:     envVar,
		})
	}
	return entries
}

func isAPIKeyProvider(providerID string) bool {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return false
	}
	if spec.Auth.Type == core.ProviderAuthTypeAPIKey && spec.Auth.APIKeyEnv != "" {
		return true
	}
	return dashboardWidget(providerID).APIKeyEnv != ""
}

func envVarForProvider(providerID string) string {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return ""
	}
	if spec.Auth.APIKeyEnv != "" {
		return spec.Auth.APIKeyEnv
	}
	return dashboardWidget(providerID).APIKeyEnv
}
