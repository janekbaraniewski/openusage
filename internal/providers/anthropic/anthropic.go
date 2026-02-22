package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const defaultBaseURL = "https://api.anthropic.com/v1"

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "anthropic",
			Info: core.ProviderInfo{
				Name:         "Anthropic",
				Capabilities: []string{"headers"},
				DocURL:       "https://docs.anthropic.com/en/api/rate-limits",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "ANTHROPIC_API_KEY",
				DefaultAccountID: "anthropic",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set ANTHROPIC_API_KEY to a valid Anthropic API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRolePeach)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.UsageSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusAuth,
			Message:    "no API key found (set ANTHROPIC_API_KEY or configure token)",
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	headers := map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": "2023-06-01",
		"Content-Type":      "application/json",
	}

	req, err := shared.CreateStandardRequest(ctx, baseURL, "/messages", apiKey, headers)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("anthropic: creating request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap, err := shared.ProcessStandardResponse(resp, acct, p.ID())
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("anthropic: processing response: %w", err)
	}

	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"anthropic-ratelimit-requests-limit",
		"anthropic-ratelimit-requests-remaining",
		"anthropic-ratelimit-requests-reset")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"anthropic-ratelimit-tokens-limit",
		"anthropic-ratelimit-tokens-remaining",
		"anthropic-ratelimit-tokens-reset")

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}
