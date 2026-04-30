package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/config"
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
				Type:                core.ProviderAuthTypeAPIKey,
				APIKeyEnv:           "OPENCODE_API_KEY",
				DefaultAccountID:    "opencode",
				SupplementalTypes:   []core.ProviderAuthType{core.ProviderAuthTypeBrowserSession},
				BrowserCookieDomain: ".opencode.ai",
				BrowserCookieName:   "auth",
				BrowserConsoleURL:   "https://opencode.ai/auth",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set OPENCODE_API_KEY (or ZEN_API_KEY) with your OpenCode Zen key for chat-surface auth.",
					"For balance / monthly usage / subscription data: open Settings → 5 KEYS, navigate to opencode-console, and press Enter to import the session cookie from your browser.",
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

	// Optional: enrich the snapshot with console-side data (balance,
	// monthly usage, subscription) when a browser-session cookie is
	// configured for this account. Failures are non-fatal — the
	// API-key probe already succeeded above, the snapshot is in a good
	// state, we just skip the enrichment and surface a hint.
	if err := p.enrichFromConsole(ctx, acct, &snap); err != nil {
		// Distinguish "no cookie configured" (silent) from "cookie
		// rejected" (loud diagnostic for the tile).
		var authErr *ConsoleAuthError
		switch {
		case errors.As(err, &authErr):
			snap.SetDiagnostic("opencode_console_auth_error", authErr.Error())
			snap.Raw["console_auth_status"] = fmt.Sprintf("%d", authErr.StatusCode)
		case errors.Is(err, errNoCookieConfigured):
			// expected when user hasn't connected a browser session
		default:
			snap.Raw["console_error"] = err.Error()
		}
	}

	shared.FinalizeStatus(&snap)
	if snap.Status == core.StatusOK {
		if bal, ok := snap.Metrics["console_balance"]; ok && bal.Remaining != nil {
			snap.Message = fmt.Sprintf("$%.2f balance · %d Zen models", *bal.Remaining, len(models.Data))
		} else {
			snap.Message = fmt.Sprintf("Auth OK · %d Zen models", len(models.Data))
		}
	}
	return snap, nil
}

var errNoCookieConfigured = errors.New("opencode: no browser session configured")

// enrichFromConsole loads the stored browser session for the account, calls
// the OpenCode console RPCs, and merges the results into the snapshot's
// metrics + attributes. Returns errNoCookieConfigured when the user hasn't
// opted in to browser-session auth.
func (p *Provider) enrichFromConsole(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) error {
	session, ok, err := config.LoadSession(acct.ID)
	if err != nil || !ok || session.Value == "" {
		return errNoCookieConfigured
	}
	workspaceID := strings.TrimSpace(acct.Hint("opencode_workspace_id", ""))
	if workspaceID == "" {
		// First-time setup: workspace id discovery requires another RPC
		// call we haven't implemented yet (queryWorkspaces). For now, the
		// account config can override it via a "opencode_workspace_id"
		// extra-data hint. Without it we surface a clear diagnostic
		// rather than silently noop.
		snap.SetDiagnostic("opencode_console_setup_hint", "set extra_data.opencode_workspace_id on the account to enable console enrichment")
		return errNoCookieConfigured
	}

	client := NewConsoleClient(session.Value, session.CookieName, workspaceID)
	billing, err := client.QueryBillingInfo(ctx)
	if err != nil {
		return err
	}

	// Map billing fields into provider-tile metric keys. Cents-based
	// internal representation (formatBalance / 1e8 in OpenCode's UI) is
	// kept as raw numbers in our snapshots; the dashboard widget will
	// format them.
	available := billing.Balance / 1e8
	usage := billing.MonthlyUsage / 1e8
	snap.Metrics["console_balance"] = core.Metric{
		Remaining: core.Float64Ptr(available),
		Unit:      "USD",
		Window:    "current",
	}
	if billing.MonthlyLimit != nil {
		limit := *billing.MonthlyLimit / 1e8
		snap.Metrics["monthly_limit"] = core.Metric{
			Limit:     core.Float64Ptr(limit),
			Used:      core.Float64Ptr(usage),
			Remaining: core.Float64Ptr(limit - usage),
			Unit:      "USD",
			Window:    "month",
		}
	}
	snap.Metrics["monthly_usage"] = core.Metric{
		Used:   core.Float64Ptr(usage),
		Unit:   "USD",
		Window: "month",
	}
	snap.Metrics["reload_amount"] = core.Metric{
		Limit: core.Float64Ptr(billing.ReloadAmount / 1e8),
		Unit:  "USD",
	}
	snap.Metrics["reload_trigger"] = core.Metric{
		Limit: core.Float64Ptr(billing.ReloadTrigger / 1e8),
		Unit:  "USD",
	}

	if billing.SubscriptionPlan != "" {
		snap.SetAttribute("subscription_plan", billing.SubscriptionPlan)
	}
	if billing.HasSubscription {
		snap.SetAttribute("subscription_status", "active")
	}
	if billing.PaymentMethodLast4 != "" {
		snap.SetAttribute("payment_method_last4", billing.PaymentMethodLast4)
	}
	if billing.PaymentMethodType != "" {
		snap.SetAttribute("payment_method_type", billing.PaymentMethodType)
	}
	snap.SetAttribute("auth_scope", "zen+console")
	snap.SetAttribute("console_session_browser", session.SourceBrowser)

	return nil
}
