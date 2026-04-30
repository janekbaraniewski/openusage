// Package perplexity implements a usage provider for the Perplexity API
// platform. Perplexity's public API is purely chat-completion — there's no
// `/usage` or `/credits` endpoint behind API-key auth (verified against
// docs.perplexity.ai/llms.txt + their published OpenAPI spec). Real billing
// data lives behind session-cookie console RPCs at console.perplexity.ai/
// rest/pplx-api/v2/groups/<org_id>/...
//
// This provider is browser-session-auth-primary: there is no API-key-based
// fallback that would surface anything useful, so we don't pretend to have
// one. When the user hasn't connected via browser, the tile sits in AUTH
// state with a clear hint pointing to Settings → 5 KEYS.
package perplexity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	consoleBaseURL = "https://console.perplexity.ai"

	// Pinned organization-list endpoint — first call, gives us the
	// orgID(s) the user has access to. Subsequent endpoints take the
	// orgID in their path.
	groupsListPath = "/rest/pplx-api/v2/groups"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "perplexity",
			Info: core.ProviderInfo{
				Name:         "Perplexity",
				Capabilities: []string{"console_billing", "console_usage_analytics", "tier_progression"},
				DocURL:       "https://docs.perplexity.ai/docs/admin/rate-limits-usage-tiers",
			},
			Auth: core.ProviderAuthSpec{
				Type:                core.ProviderAuthTypeBrowserSession,
				DefaultAccountID:    "perplexity",
				BrowserCookieDomain: ".perplexity.ai",
				BrowserCookieName:   "__Secure-next-auth.session-token",
				BrowserConsoleURL:   "https://console.perplexity.ai/",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Log into https://console.perplexity.ai in your browser.",
					"In openusage: Settings → 5 KEYS → perplexity → press Enter to import the session cookie.",
					"Tile shows your tier, balance, monthly usage, and per-model spend once connected.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

// Fetch is browser-session-auth only. If no session is configured, return a
// clear AUTH snapshot pointing the user at the connect flow.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	session, ok, err := config.LoadSession(acct.ID)
	if err != nil || !ok || session.Value == "" {
		snap.Status = core.StatusAuth
		snap.Message = "browser session not configured — Settings → 5 KEYS → perplexity → Enter"
		return snap, nil
	}

	client := newConsoleClient(session.Value, session.CookieName)
	snap.SetAttribute("auth_scope", "console_session")
	snap.SetAttribute("console_session_browser", session.SourceBrowser)

	// Step 1: discover the user's API org(s).
	orgs, err := client.listGroups(ctx)
	if err != nil {
		var aerr *consoleAuthError
		if errors.As(err, &aerr) {
			snap.Status = core.StatusAuth
			snap.Message = "session expired — re-login at console.perplexity.ai"
			return snap, nil
		}
		return snap, fmt.Errorf("perplexity: list groups: %w", err)
	}
	if len(orgs.Orgs) == 0 {
		snap.Status = core.StatusError
		snap.Message = "no Perplexity API orgs found on this account"
		return snap, nil
	}
	// First org is the default (per their UI ordering); user can override
	// via account.extra_data.perplexity_org_id.
	orgID := strings.TrimSpace(acct.Hint("perplexity_org_id", ""))
	if orgID == "" {
		for _, o := range orgs.Orgs {
			if o.IsDefaultOrg {
				orgID = o.APIOrgID
				break
			}
		}
		if orgID == "" {
			orgID = orgs.Orgs[0].APIOrgID
		}
	}
	snap.SetAttribute("org_id", orgID)
	for _, o := range orgs.Orgs {
		if o.APIOrgID == orgID {
			snap.SetAttribute("org_display_name", o.DisplayName)
			snap.Metrics["usage_tier"] = core.Metric{
				Used: core.Float64Ptr(float64(o.RuntimeSettings.UsageTier)),
				Unit: "tier",
			}
			snap.SetAttribute("usage_tier", fmt.Sprintf("%d", o.RuntimeSettings.UsageTier))
			break
		}
	}

	// Step 2: fetch the rich org info (balance, payment method, spend).
	info, err := client.getGroupDetail(ctx, orgID)
	if err != nil {
		var aerr *consoleAuthError
		if errors.As(err, &aerr) {
			snap.Status = core.StatusAuth
			snap.Message = "session expired"
			return snap, nil
		}
		// Soft failure: groups list worked but detail didn't. Surface the
		// tier we already have and skip the rest.
		snap.Raw["console_detail_error"] = err.Error()
	} else {
		snap.Metrics["available_balance"] = core.Metric{
			Remaining: core.Float64Ptr(info.CustomerInfo.Balance),
			Unit:      "USD",
			Window:    "current",
		}
		if info.CustomerInfo.PendingBalance != 0 {
			snap.Metrics["pending_balance"] = core.Metric{
				Used: core.Float64Ptr(info.CustomerInfo.PendingBalance),
				Unit: "USD",
			}
		}
		if info.CustomerInfo.AutoTopUpAmount > 0 {
			snap.Metrics["auto_top_up_amount"] = core.Metric{
				Limit: core.Float64Ptr(info.CustomerInfo.AutoTopUpAmount),
				Unit:  "USD",
			}
		}
		if info.CustomerInfo.AutoTopUpThreshold > 0 {
			snap.Metrics["auto_top_up_threshold"] = core.Metric{
				Limit: core.Float64Ptr(info.CustomerInfo.AutoTopUpThreshold),
				Unit:  "USD",
			}
		}
		if info.CustomerInfo.Spend.TotalSpend > 0 {
			snap.Metrics["total_spend"] = core.Metric{
				Used: core.Float64Ptr(info.CustomerInfo.Spend.TotalSpend),
				Unit: "USD",
			}
		}
		if info.CustomerInfo.IsPro {
			snap.SetAttribute("is_pro", "true")
		}
		if info.CustomerInfo.ContactInfo.Email != "" {
			snap.SetAttribute("account_email", info.CustomerInfo.ContactInfo.Email)
		}
		if info.CustomerInfo.ContactInfo.Country != "" {
			snap.SetAttribute("account_country", info.CustomerInfo.ContactInfo.Country)
		}
		if info.DefaultPaymentMethodCard.LastDigits != "" {
			snap.SetAttribute("payment_method_last4", info.DefaultPaymentMethodCard.LastDigits)
			snap.SetAttribute("payment_method_brand", info.DefaultPaymentMethodCard.Brand)
		}
	}

	// Step 3: usage analytics — meter-event time-series. Each meter has a
	// name (api_requests, input_tokens, output_tokens, ...) and event
	// summaries grouped by model_name + api_key_suffix.
	analytics, err := client.getUsageAnalytics(ctx, orgID, "day", "past_month")
	if err != nil {
		// Non-fatal — analytics often empty for fresh accounts.
		snap.Raw["console_analytics_error"] = err.Error()
	} else {
		applyAnalytics(&snap, analytics)
	}

	shared.FinalizeStatus(&snap)
	if snap.Status == core.StatusOK {
		if bal, ok := snap.Metrics["available_balance"]; ok && bal.Remaining != nil {
			snap.Message = fmt.Sprintf("$%.2f balance · Tier %s", *bal.Remaining, snap.Attributes["usage_tier"])
		} else {
			snap.Message = "console connected"
		}
	}
	return snap, nil
}

// applyAnalytics walks the meter-events response and aggregates by metric
// name into the snapshot.
func applyAnalytics(snap *core.UsageSnapshot, analytics []meter) {
	for _, m := range analytics {
		var total float64
		for _, ev := range m.MeterEventSummaries {
			total += ev.Value
		}
		if total == 0 {
			continue
		}
		switch m.Name {
		case "api_requests":
			snap.Metrics["requests_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "requests", Window: "30d"}
		case "input_tokens":
			snap.Metrics["input_tokens_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "tokens", Window: "30d"}
		case "output_tokens":
			snap.Metrics["output_tokens_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "tokens", Window: "30d"}
		case "citation_tokens":
			snap.Metrics["citation_tokens_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "tokens", Window: "30d"}
		case "reasoning_tokens":
			snap.Metrics["reasoning_tokens_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "tokens", Window: "30d"}
		case "num_search_queries", "search_request_count":
			snap.Metrics["search_queries_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "queries", Window: "30d"}
		case "pro_search_request_count":
			snap.Metrics["pro_search_window"] = core.Metric{Used: core.Float64Ptr(total), Unit: "requests", Window: "30d"}
		}
	}
}

// ===== Console RPC client + types =====

// consoleClient is a thin HTTP client for console.perplexity.ai with cookie
// auth. Mirrors the headers the SPA sends (Next-auth session cookie + the
// x-app-* trio that some endpoints expect). All endpoints are JSON GETs;
// no CSRF token is required for reads.
type consoleClient struct {
	httpClient  *http.Client
	baseURL     string
	cookieName  string
	cookieValue string
}

func newConsoleClient(cookieValue, cookieName string) *consoleClient {
	if cookieName == "" {
		cookieName = "__Secure-next-auth.session-token"
	}
	base := consoleBaseURL
	// Test seam: tests override the base URL by setting this env var.
	// Production never sets it, so the constant wins.
	if override := strings.TrimSpace(os.Getenv("OPENUSAGE_PERPLEXITY_CONSOLE_BASE_URL")); override != "" {
		base = override
	}
	return &consoleClient{
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		baseURL:     base,
		cookieName:  cookieName,
		cookieValue: cookieValue,
	}
}

func (c *consoleClient) get(ctx context.Context, path string, query map[string]string, out any) error {
	full := c.baseURL + path + "?version=2.18&source=default"
	for k, v := range query {
		full += "&" + k + "=" + v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, full, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-app-apiclient", "default")
	req.Header.Set("x-app-apiversion", "2.18")
	req.Header.Set("x-app-domain", "api-console")
	req.Header.Set("User-Agent", "openusage/perplexity-console")
	req.AddCookie(&http.Cookie{Name: c.cookieName, Value: c.cookieValue})

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("perplexity console: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &consoleAuthError{StatusCode: resp.StatusCode, Body: shorten(body)}
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("perplexity console: HTTP %d (%s)", resp.StatusCode, shorten(body))
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("perplexity console: decode %s: %w", path, err)
	}
	return nil
}

type consoleAuthError struct {
	StatusCode int
	Body       string
}

func (e *consoleAuthError) Error() string {
	return fmt.Sprintf("perplexity console auth failed: HTTP %d (%s)", e.StatusCode, e.Body)
}

func shorten(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ===== Wire types matching the Perplexity console responses =====

type groupsListResponse struct {
	Orgs []group `json:"orgs"`
}

type group struct {
	APIOrgID        string          `json:"api_org_id"`
	DisplayName     string          `json:"display_name"`
	UserRole        string          `json:"user_role"`
	IsDefaultOrg    bool            `json:"is_default_org"`
	RuntimeSettings runtimeSettings `json:"runtime_settings"`
}

type runtimeSettings struct {
	UsageTier int `json:"usage_tier"`
}

type groupDetailResponse struct {
	APIOrganization          group           `json:"apiOrganization"`
	CustomerInfo             customerInfo    `json:"customerInfo"`
	HasDefaultPaymentMethod  bool            `json:"hasDefaultPaymentMethod"`
	DefaultPaymentMethodCard paymentCard     `json:"defaultPaymentMethodCard"`
}

type customerInfo struct {
	UserID             string      `json:"user_id"`
	Name               string      `json:"name"`
	ContactInfo        contactInfo `json:"contact_info"`
	IsPro              bool        `json:"is_pro"`
	AutoTopUpAmount    float64     `json:"auto_top_up_amount"`
	AutoTopUpThreshold float64     `json:"auto_top_up_threshold"`
	Balance            float64     `json:"balance"`
	PendingBalance     float64     `json:"pending_balance"`
	Spend              spendBlock  `json:"spend"`
}

type contactInfo struct {
	Email   string `json:"email"`
	Country string `json:"country"`
}

type spendBlock struct {
	TotalSpend float64 `json:"total_spend"`
}

type paymentCard struct {
	ID         string `json:"id"`
	Brand      string `json:"brand"`
	LastDigits string `json:"last_digits"`
}

type meter struct {
	ID                  string         `json:"id"`
	Name                string         `json:"name"`
	DimensionGroupByKeys []string       `json:"dimension_group_by_keys"`
	MeterEventSummaries  []meterSummary `json:"meter_event_summaries"`
}

type meterSummary struct {
	Value      float64           `json:"value"`
	StartTime  string            `json:"start_time"`
	EndTime    string            `json:"end_time"`
	Dimensions map[string]string `json:"dimensions"`
}

func (c *consoleClient) listGroups(ctx context.Context) (groupsListResponse, error) {
	var out groupsListResponse
	if err := c.get(ctx, "/rest/pplx-api/v2/groups", nil, &out); err != nil {
		return groupsListResponse{}, err
	}
	return out, nil
}

func (c *consoleClient) getGroupDetail(ctx context.Context, orgID string) (groupDetailResponse, error) {
	var out groupDetailResponse
	path := "/rest/pplx-api/v2/groups/" + orgID
	if err := c.get(ctx, path, nil, &out); err != nil {
		return groupDetailResponse{}, err
	}
	return out, nil
}

func (c *consoleClient) getUsageAnalytics(ctx context.Context, orgID, bucket, timeRange string) ([]meter, error) {
	var out []meter
	path := "/rest/pplx-api/v2/groups/" + orgID + "/usage-analytics"
	q := map[string]string{
		"time_bucket": bucket,
		"time_range":  timeRange,
	}
	if err := c.get(ctx, path, q, &out); err != nil {
		return nil, err
	}
	return out, nil
}
