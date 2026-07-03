package azure_openai

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	// defaultAPIVersion is a GA data-plane version that supports the
	// "list deployments" operation used for the lightweight probe.
	defaultAPIVersion = "2024-10-21"

	// endpointEnv holds a full Azure OpenAI resource endpoint (e.g.
	// https://my-resource.openai.azure.com). It is an override for
	// non-standard endpoints (sovereign clouds, custom domains) and, like
	// the account's base_url, takes precedence over resourceNameEnv.
	endpointEnv = "AZURE_OPENAI_ENDPOINT"

	// resourceNameEnv holds a bare Azure resource name (e.g. my-resource),
	// matching OpenCode's AZURE_RESOURCE_NAME convention. When set — and no
	// explicit endpoint override (base_url / AZURE_OPENAI_ENDPOINT) is given —
	// the standard endpoint https://<name>.openai.azure.com is built from it.
	// Sharing this env var with OpenCode lets one configuration drive both
	// tools.
	resourceNameEnv = "AZURE_RESOURCE_NAME"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "azure_openai",
			Info: core.ProviderInfo{
				Name:         "Azure OpenAI",
				Capabilities: []string{"headers"},
				DocURL:       "https://learn.microsoft.com/azure/ai-services/openai/quotas-limits",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "AZURE_OPENAI_API_KEY",
				DefaultAccountID: "azure_openai",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set AZURE_OPENAI_API_KEY (or AZURE_API_KEY) to a valid Azure OpenAI key.",
					"Set AZURE_RESOURCE_NAME to your resource name (e.g. my-resource) — OpenUsage builds https://my-resource.openai.azure.com from it.",
					"For non-standard endpoints (sovereign clouds, custom domains), set AZURE_OPENAI_ENDPOINT or base_url to the full URL instead.",
				},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleBlue)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := resolveEndpoint(acct)
	if baseURL == "" {
		snap := core.NewAuthSnapshot(p.ID(), acct.ID,
			fmt.Sprintf("no endpoint configured (set %s, %s, or base_url)", resourceNameEnv, endpointEnv))
		return snap, nil
	}

	// Azure OpenAI authenticates with the "api-key" header, not a Bearer
	// token, and requires an explicit api-version. Probe the deployments
	// listing endpoint: it is a lightweight GET that validates auth and
	// surfaces any x-ratelimit-* headers without spending tokens.
	url := baseURL + "/openai/deployments?api-version=" + defaultAPIVersion
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("azure_openai: creating request: %w", err)
	}
	req.Header.Set("api-key", apiKey)

	resp, err := p.Client().Do(req)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("azure_openai: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap, err := shared.ProcessStandardResponse(resp, acct, p.ID())
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("azure_openai: processing response: %w", err)
	}

	shared.ApplyStandardRateLimits(resp, &snap)
	shared.FinalizeStatus(&snap)
	return snap, nil
}

// resolveEndpoint returns the Azure OpenAI resource endpoint. Resolution order:
//
//  1. The account's base_url (explicit override).
//  2. The AZURE_OPENAI_ENDPOINT env var (explicit override).
//  3. A standard endpoint built from AZURE_RESOURCE_NAME
//     (https://<name>.openai.azure.com), matching OpenCode's convention.
//
// The trailing slash is trimmed so path joins stay well-formed.
func resolveEndpoint(acct core.AccountConfig) string {
	if endpoint := strings.TrimSpace(acct.BaseURL); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	if endpoint := strings.TrimSpace(os.Getenv(endpointEnv)); endpoint != "" {
		return strings.TrimRight(endpoint, "/")
	}
	if name := strings.TrimSpace(os.Getenv(resourceNameEnv)); name != "" {
		return "https://" + name + ".openai.azure.com"
	}
	return ""
}
