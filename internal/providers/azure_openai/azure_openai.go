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

	// endpointEnv is the environment variable holding the Azure OpenAI
	// resource endpoint (e.g. https://my-resource.openai.azure.com) when it
	// is not configured via the account's base_url.
	endpointEnv = "AZURE_OPENAI_ENDPOINT"
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
					"Set AZURE_OPENAI_API_KEY to a valid Azure OpenAI key.",
					"Set AZURE_OPENAI_ENDPOINT (or base_url) to your resource endpoint, e.g. https://my-resource.openai.azure.com.",
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
			fmt.Sprintf("no endpoint configured (set %s or base_url)", endpointEnv))
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

// resolveEndpoint returns the Azure OpenAI resource endpoint, preferring the
// account's base_url and falling back to the AZURE_OPENAI_ENDPOINT env var.
// The trailing slash is trimmed so path joins stay well-formed.
func resolveEndpoint(acct core.AccountConfig) string {
	endpoint := acct.BaseURL
	if endpoint == "" {
		endpoint = os.Getenv(endpointEnv)
	}
	return strings.TrimRight(strings.TrimSpace(endpoint), "/")
}
