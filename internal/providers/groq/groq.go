package groq

import (
	"context"
	"fmt"
	"net/http"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const defaultBaseURL = "https://api.groq.com/openai/v1"

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "groq",
			Info: core.ProviderInfo{
				Name:         "Groq",
				Capabilities: []string{"headers", "daily_limits"},
				DocURL:       "https://console.groq.com/docs/rate-limits",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "GROQ_API_KEY",
				DefaultAccountID: "groq",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set GROQ_API_KEY to a valid Groq API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleYellow)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	req, err := shared.CreateStandardRequest(ctx, baseURL, "/models", apiKey, nil)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("groq: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("groq: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap, _ := shared.ProcessStandardResponse(resp, acct, p.ID())
	shared.ApplyStandardRateLimits(resp, &snap)
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpd", "requests", "1d",
		"x-ratelimit-limit-requests-day", "x-ratelimit-remaining-requests-day", "x-ratelimit-reset-requests-day")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpd", "tokens", "1d",
		"x-ratelimit-limit-tokens-day", "x-ratelimit-remaining-tokens-day", "x-ratelimit-reset-tokens-day")

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = buildStatusMessage(snap)
	}

	return snap, nil
}

func buildStatusMessage(snap core.UsageSnapshot) string {
	var parts []string
	for _, key := range []string{"rpm", "rpd"} {
		if m, ok := snap.Metrics[key]; ok && m.Remaining != nil && m.Limit != nil {
			label := "RPM"
			if key == "rpd" {
				label = "RPD"
			}
			parts = append(parts, fmt.Sprintf("%.0f/%.0f %s", *m.Remaining, *m.Limit, label))
		}
	}
	if len(parts) == 0 {
		return "OK"
	}
	msg := "Remaining: " + parts[0]
	for _, p := range parts[1:] {
		msg += ", " + p
	}
	return msg
}
