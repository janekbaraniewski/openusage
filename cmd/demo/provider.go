package main

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

var demoProviderIDs = map[string]bool{
	"gemini_cli":  true,
	"copilot":     true,
	"cursor":      true,
	"claude_code": true,
	"codex":       true,
	"openrouter":  true,
	"ollama":      true,
}

type demoProvider struct {
	base core.UsageProvider
}

func buildDemoProviders(realProviders []core.UsageProvider) []core.UsageProvider {
	out := make([]core.UsageProvider, 0, len(realProviders))
	for _, provider := range realProviders {
		out = append(out, &demoProvider{base: provider})
	}
	return out
}

func buildDemoAccounts() []core.AccountConfig {
	providerList := providers.AllProviders()
	accounts := make([]core.AccountConfig, 0, len(demoProviderIDs))
	seenAccountIDs := make(map[string]bool, len(demoProviderIDs))
	for _, provider := range providerList {
		if !demoProviderIDs[provider.ID()] {
			continue
		}
		spec := provider.Spec()
		accountID := demoAccountID(provider.ID())
		if accountID == "" {
			accountID = spec.Auth.DefaultAccountID
		}
		if accountID == "" {
			accountID = provider.ID()
		}
		if seenAccountIDs[accountID] {
			accountID = provider.ID()
		}

		accounts = append(accounts, core.AccountConfig{
			ID:        accountID,
			Provider:  provider.ID(),
			Auth:      string(spec.Auth.Type),
			APIKeyEnv: spec.Auth.APIKeyEnv,
		})
		seenAccountIDs[accountID] = true
	}
	return accounts
}

func (p *demoProvider) ID() string {
	return p.base.ID()
}

func (p *demoProvider) Describe() core.ProviderInfo {
	return p.base.Describe()
}

func (p *demoProvider) Spec() core.ProviderSpec {
	return p.base.Spec()
}

func (p *demoProvider) DashboardWidget() core.DashboardWidget {
	return p.base.DashboardWidget()
}

func (p *demoProvider) DetailWidget() core.DetailWidget {
	return p.base.DetailWidget()
}

func (p *demoProvider) Fetch(_ context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snaps := buildDemoSnapshots()
	if snap, ok := snaps[acct.ID]; ok && snap.ProviderID == p.base.ID() {
		return forceAccountAndProvider(snap, acct.ID, p.base.ID()), nil
	}

	for _, snap := range snaps {
		if snap.ProviderID == p.base.ID() {
			return forceAccountAndProvider(snap, acct.ID, p.base.ID()), nil
		}
	}

	now := time.Now()
	return core.UsageSnapshot{
		ProviderID: p.base.ID(),
		AccountID:  acct.ID,
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
		Message:    "Demo data",
	}, nil
}

func forceAccountAndProvider(snap core.UsageSnapshot, accountID, providerID string) core.UsageSnapshot {
	snap.AccountID = accountID
	snap.ProviderID = providerID
	return snap
}

func demoAccountID(providerID string) string {
	switch providerID {
	case "claude_code":
		return "claude-code"
	case "codex":
		return "codex-cli"
	case "cursor":
		return "cursor-ide"
	case "gemini_cli":
		return "gemini-cli"
	case "openrouter":
		return "openrouter"
	case "copilot":
		return "copilot"
	case "ollama":
		return "ollama"
	default:
		return providerID
	}
}
