package openai

import (
	"context"
	"fmt"
	"net/http"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4.1-mini"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "openai",
			Info: core.ProviderInfo{
				Name:         "OpenAI",
				Capabilities: []string{"headers"},
				DocURL:       "https://platform.openai.com/docs/guides/rate-limits",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OPENAI_API_KEY",
				DefaultAccountID: "openai",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set OPENAI_API_KEY to a valid OpenAI API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleGreen)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	model := acct.ProbeModel
	if model == "" {
		model = defaultModel
	}

	headers := map[string]string{
		"Authorization": "Bearer " + apiKey,
	}

	req, err := shared.CreateStandardRequest(ctx, baseURL, "/models/"+model, apiKey, headers)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap, err := shared.ProcessStandardResponse(resp, acct, p.ID())
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("openai: processing response: %w", err)
	}

	shared.ApplyStandardRateLimits(resp, &snap)
	shared.FinalizeStatus(&snap)
	return snap, nil
}
