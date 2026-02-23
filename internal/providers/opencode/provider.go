package opencode

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/openrouter"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

type Provider struct {
	providerbase.Base
	delegate core.UsageProvider
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "opencode",
			Info: core.ProviderInfo{
				Name:         "OpenCode",
				Capabilities: []string{"openrouter_compatible", "credits_endpoint", "activity_endpoint", "generation_stats"},
				DocURL:       "https://opencode.ai/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OPENCODE_API_KEY",
				DefaultAccountID: "opencode",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set OPENCODE_API_KEY (or ZEN_API_KEY) with your OpenCode gateway key.",
					"Optionally set a custom account base URL if your gateway endpoint differs.",
				},
			},
		}),
		delegate: openrouter.New(),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap, err := p.delegate.Fetch(ctx, acct)
	if err != nil {
		return snap, err
	}
	snap.ProviderID = p.ID()
	return snap, nil
}
