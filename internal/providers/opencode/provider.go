package opencode

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// OpenCode Zen exposes only OpenAI-compatible chat/messages/models endpoints
// behind its API-key auth (verified via reverse-engineering against the
// upstream source at github.com/anomalyco/opencode). Billing, usage history,
// and key management live behind session-cookie SolidStart RPCs that this
// provider does not (yet) authenticate against — those would need a separate
// cookie-based code path.
//
// As a result, the only signal we get from a poll is "is this key valid?".
// Tile metrics (token spend, model burn, project breakdown, tool usage,
// activity totals) come from the OpenCode telemetry plugin and flow in via
// the telemetry pipeline once an account with provider_id=opencode exists.
const (
	defaultBaseURL = "https://opencode.ai"
	modelsPath     = "/zen/v1/models"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "opencode",
			Info: core.ProviderInfo{
				Name:         "OpenCode",
				Capabilities: []string{"zen_models_endpoint", "telemetry_driven_metrics"},
				DocURL:       "https://opencode.ai/docs/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "OPENCODE_API_KEY",
				DefaultAccountID: "opencode",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set OPENCODE_API_KEY (or ZEN_API_KEY) with your OpenCode Zen key.",
					"Tile spend / model / activity metrics are populated from the OpenCode telemetry plugin; see Settings → 7 INTEG.",
				},
			},
		}),
	}
}

type modelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.SetAttribute("auth_scope", "zen")
	snap.SetAttribute("api_base_url", baseURL)

	var models modelsResponse
	statusCode, _, err := shared.FetchJSON(ctx, baseURL+modelsPath, apiKey, &models, p.Client())
	if err != nil {
		switch statusCode {
		case http.StatusUnauthorized, http.StatusForbidden:
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("HTTP %d – check OPENCODE_API_KEY", statusCode)
			return snap, nil
		case http.StatusTooManyRequests:
			snap.Status = core.StatusLimited
			snap.Message = "rate limited (HTTP 429)"
			return snap, nil
		}
		return snap, fmt.Errorf("opencode zen models: %w", err)
	}

	if len(models.Data) > 0 {
		ids := make([]string, 0, len(models.Data))
		for _, m := range models.Data {
			if id := strings.TrimSpace(m.ID); id != "" {
				ids = append(ids, id)
			}
		}
		snap.SetAttribute("available_models", strings.Join(ids, ", "))
		snap.SetAttribute("available_models_count", fmt.Sprintf("%d", len(ids)))
	}

	shared.FinalizeStatus(&snap)
	if snap.Status == core.StatusOK {
		snap.Message = fmt.Sprintf("Auth OK · %d Zen models", len(models.Data))
	}
	return snap, nil
}
