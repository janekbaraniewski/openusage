package opencode

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

var (
	loadBrowserSession = shared.LoadOrRefreshBrowserSession
	newConsoleClient   = NewConsoleClient
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
				Capabilities: []string{"zen_models_endpoint", "telemetry_driven_metrics", "console_billing", "subscription_usage"},
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
					"For balance / monthly usage / subscription data: open Settings → 5 KEYS, highlight opencode, and press c to import the session cookie from your browser.",
					"Tile spend / model / activity metrics are populated from the OpenCode telemetry plugin; see Settings → 7 INTEG.",
				},
			},
			Dashboard: providerbase.DefaultDashboard(
				providerbase.WithColorRole(core.DashboardColorRoleBlue),
				providerbase.WithGaugePriority("rolling_usage", "weekly_usage", "monthly_usage_pct", "console_balance", "monthly_limit"),
				// OpenCode Zen quota has three meaningful usage-window
				// percentages (5h / 7d / ~30d monthly) — the default cap of 2
				// gauge lines would always bump the monthly figure in favor
				// of the two shorter windows.
				providerbase.WithGaugeMaxLines(3),
				providerbase.WithCompactRows(
					core.DashboardCompactRow{
						Label:       "Quota",
						Keys:        []string{"rolling_usage", "weekly_usage", "monthly_usage_pct"},
						MaxSegments: 3,
					},
					core.DashboardCompactRow{
						Label:       "Credits",
						Keys:        []string{"console_balance", "monthly_usage", "monthly_limit"},
						MaxSegments: 3,
					},
				),
				providerbase.WithMetricLabels(map[string]string{
					"rolling_usage":     "5h Usage",
					"weekly_usage":      "Weekly",
					"monthly_usage_pct": "Monthly",
					"console_balance":   "Balance",
					"monthly_usage":     "Month Spend",
					"monthly_limit":     "Month Limit",
					"reload_amount":     "Reload",
					"reload_trigger":    "Reload At",
				}),
				providerbase.WithCompactLabels(map[string]string{
					"rolling_usage":     "5h",
					"weekly_usage":      "wk",
					"monthly_usage_pct": "mo",
					"console_balance":   "bal",
					"monthly_usage":     "mo$",
					"monthly_limit":     "cap",
				}),
			),
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
	hasBrowserSession := acct.BrowserCookie != nil

	// If no API key and no browser session configured, require auth.
	if authSnap != nil && !hasBrowserSession {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.SetAttribute("auth_scope", "zen")
	snap.SetAttribute("api_base_url", baseURL)

	// Try Zen API key probe if we have a key.
	if authSnap == nil {
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
			// If we have a browser session, don't fail on Zen API errors —
			// the console enrichment can still provide useful data.
			if !hasBrowserSession {
				return snap, fmt.Errorf("opencode zen models: %w", err)
			}
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
	}

	// Optional: enrich the snapshot with console-side data (balance,
	// monthly usage, subscription) when a browser-session cookie is
	// configured for this account. Failures are non-fatal when we already
	// have a validated Zen API key — the snapshot is in a good state, we
	// just skip the enrichment and surface a hint.
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

		// We never validated a Zen API key (authSnap != nil, so the probe
		// above was skipped) and console enrichment also failed to produce
		// usable data. Without this, shared.FinalizeStatus would default
		// the still-empty snap.Status to StatusOK — reporting a healthy
		// tile for an account that has neither a working API key nor a
		// working browser session.
		if authSnap != nil && snap.Status == "" {
			snap.Status = authSnap.Status
			if authSnap.Message != "" {
				snap.Message = authSnap.Message
			}
		}
	}

	shared.FinalizeStatus(&snap)
	if snap.Status == core.StatusOK {
		modelCount := snap.Attributes["available_models_count"]
		if bal, ok := snap.Metrics["console_balance"]; ok && bal.Remaining != nil {
			msg := fmt.Sprintf("$%.2f balance", *bal.Remaining)
			if modelCount != "" {
				msg += fmt.Sprintf(" · %s Zen models", modelCount)
			}
			if rolling, ok := snap.Metrics["rolling_usage"]; ok && rolling.Used != nil {
				msg += fmt.Sprintf(" · %.0f%% 5h", *rolling.Used)
			}
			if weekly, ok := snap.Metrics["weekly_usage"]; ok && weekly.Used != nil {
				msg += fmt.Sprintf(" · %.0f%% weekly", *weekly.Used)
			}
			snap.Message = msg
		} else if modelCount != "" {
			snap.Message = fmt.Sprintf("Auth OK · %s Zen models", modelCount)
		} else {
			snap.Message = "Auth OK"
		}
	}
	return snap, nil
}

var errNoCookieConfigured = errors.New("opencode: no browser session configured")

// loadStoredSession reads a browser session directly from the credentials file
// without refreshing from the browser. This avoids the destructive refresh in
// LoadOrRefreshBrowserSession that overwrites stored sessions when multiple
// accounts use different browsers for the same domain.
//
// A package-level var (not a plain func) so tests can stub it — mirrors the
// loadBrowserSession/newConsoleClient seams above.
var loadStoredSession = func(accountID string) (config.BrowserSession, bool, error) {
	return config.LoadSession(accountID)
}

// enrichFromConsole loads the stored browser session for the account, calls
// the OpenCode console RPCs, and merges the results into the snapshot's
// metrics + attributes. Returns errNoCookieConfigured when the user hasn't
// opted in to browser-session auth.
func (p *Provider) enrichFromConsole(ctx context.Context, acct core.AccountConfig, snap *core.UsageSnapshot) error {
	// Load directly from stored credentials to avoid the browser refresh
	// in LoadOrRefreshBrowserSession, which can overwrite stored sessions
	// when multiple accounts use different browsers for the same domain.
	session, ok, err := loadStoredSession(acct.ID)
	if err != nil || !ok || session.Value == "" {
		return errNoCookieConfigured
	}

	client := newConsoleClient(session.Value, session.CookieName, "")
	workspaceID := strings.TrimSpace(acct.Hint("opencode_workspace_id", ""))
	if workspaceID == "" {
		workspaceID, err = client.DiscoverWorkspaceID(ctx)
		if err != nil {
			snap.SetDiagnostic("opencode_console_workspace_error", err.Error())
			return errNoCookieConfigured
		}
	}
	client.WorkspaceID = workspaceID

	// Fetch billing info and subscription usage from the Go usage page HTML.
	// This is more reliable than individual RPC calls whose content-hash
	// IDs rotate on every backend deploy. The page embeds both billing.get
	// and lite.subscription.get data in a Seroval script blob.
	type pageResult struct {
		subscription SubscriptionUsage
		billing      BillingInfo
		err          error
	}

	pageCh := make(chan pageResult, 1)
	go func() {
		sub, bill, err := client.FetchGoUsagePage(ctx, workspaceID)
		pageCh <- pageResult{sub, bill, err}
	}()

	page := <-pageCh

	// If HTML scraping fails, fall back to individual RPC calls.
	var billing BillingInfo
	var subscription SubscriptionUsage
	billingFailed := false

	if page.err != nil {
		snap.SetDiagnostic("opencode_page_scrape_error", page.err.Error())
		billingFailed = true
	} else {
		billing = page.billing
		subscription = page.subscription
	}

	// Fallback: try individual billing RPC if page scraping didn't get billing data.
	var fallbackErr error
	if billingFailed || (billing.Balance == 0 && billing.MonthlyUsage == 0 && billing.MonthlyLimit == nil) {
		billingRPC, err := client.QueryBillingInfo(ctx)
		if err == nil {
			billing = billingRPC
			billingFailed = false
		} else if billingFailed {
			// Only treat the fallback's error as fatal when the primary page
			// scrape had already failed. If the page scrape succeeded with a
			// genuine zero balance, a failing fallback isn't itself a problem.
			fallbackErr = err
		}
	}

	if billingFailed {
		// Both the page scrape and the billing RPC fallback failed. Return
		// the error instead of falling through to write zero-valued billing
		// metrics into the snapshot as if they were real data — that would
		// present "$0.00 balance" for what may actually be an expired
		// session that needs reauthentication.
		var authErr *ConsoleAuthError
		switch {
		case errors.As(fallbackErr, &authErr):
			return fallbackErr
		case errors.As(page.err, &authErr):
			return page.err
		default:
			return fmt.Errorf("opencode console: billing unavailable (page scrape: %v, fallback: %v)", page.err, fallbackErr)
		}
	}

	// Process billing info.
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

	// Process subscription usage (rolling 5h + weekly + monthly percentages).
	// Gate on the *OK presence flags, not value > 0 — a legitimate 0% reading
	// (quota window just reset) must still populate the metric.
	if subscription.RollingUsageOK || subscription.WeeklyUsageOK || subscription.MonthlyUsageOK {
		if subscription.RollingUsageOK {
			snap.Metrics["rolling_usage"] = core.Metric{
				Used:   core.Float64Ptr(subscription.RollingUsagePct),
				Limit:  core.Float64Ptr(100),
				Unit:   "percent",
				Window: "rolling-5h",
			}
			if subscription.RollingResetSec > 0 {
				snap.Resets["rolling_usage_reset"] = snap.Timestamp.Add(
					time.Duration(subscription.RollingResetSec) * time.Second)
			}
		}
		if subscription.WeeklyUsageOK {
			snap.Metrics["weekly_usage"] = core.Metric{
				Used:   core.Float64Ptr(subscription.WeeklyUsagePct),
				Limit:  core.Float64Ptr(100),
				Unit:   "percent",
				Window: "7d",
			}
			if subscription.WeeklyResetSec > 0 {
				snap.Resets["weekly_usage_reset"] = snap.Timestamp.Add(
					time.Duration(subscription.WeeklyResetSec) * time.Second)
			}
		}
		if subscription.MonthlyUsageOK {
			snap.Metrics["monthly_usage_pct"] = core.Metric{
				Used:   core.Float64Ptr(subscription.MonthlyUsagePct),
				Limit:  core.Float64Ptr(100),
				Unit:   "percent",
				Window: "month",
			}
			if subscription.MonthlyResetSec > 0 {
				snap.Resets["monthly_usage_pct_reset"] = snap.Timestamp.Add(
					time.Duration(subscription.MonthlyResetSec) * time.Second)
			}
		}
	}

	snap.SetAttribute("auth_scope", "zen+console")
	snap.SetAttribute("console_session_browser", session.SourceBrowser)

	return nil
}
