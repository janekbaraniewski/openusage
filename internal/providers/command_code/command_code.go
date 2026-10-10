// Package command_code implements a usage provider for Command Code
// (https://commandcode.ai) — the coding agent and its Provider API.
//
// Authentication is the Command Code API key. The provider resolves it from
// the account's configured token/COMMAND_CODE_API_KEY first and falls back to
// the key the CLI writes at ~/.commandcode/auth.json, so a logged-in `cmd`
// install is tracked with zero configuration.
//
// Usage comes from the same billing surface the CLI's /usage command reads:
//
//	GET /alpha/whoami?limits=1
//	GET /alpha/billing/credits
//	GET /alpha/billing/subscriptions
//	GET /alpha/usage/summary
//
// Reported: monthly credit balance, the rolling 5-hour and weekly spend
// windows (used vs cap, with reset times), plan identity, billing period, and
// billing-period/today spend, requests and tokens.
package command_code

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

// ID is the canonical provider identifier registered in the providers registry.
const ID = "command_code"

// DefaultAccountID is the account ID used by the auto-detector.
const DefaultAccountID = "command_code"

// EnvAPIKey is the documented environment variable the CLI and Provider API read.
const EnvAPIKey = "COMMAND_CODE_API_KEY"

const (
	defaultBaseURL = "https://api.commandcode.ai"

	whoamiPath       = "/alpha/whoami"
	creditsPath      = "/alpha/billing/credits"
	subscriptionPath = "/alpha/billing/subscriptions"
	summaryPath      = "/alpha/usage/summary"
)

// Plan identifiers → included monthly credits (USD of model usage). Mirrors the
// CLI's plan table; used only to give credit_balance a denominator.
var planCredits = map[string]float64{
	"individual-go":       10,
	"individual-go-v1":    10,
	"individual-goat":     70,
	"individual-pro":      30,
	"individual-pro-v1":   80,
	"individual-provider": 15,
	"individual-max":      150,
	"individual-ultra":    300,
	"teams-pro":           40,
}

var planNames = map[string]string{
	"individual-go":       "Go",
	"individual-go-v1":    "Go",
	"individual-goat":     "GOAT",
	"individual-pro":      "Pro",
	"individual-pro-v1":   "Pro",
	"individual-provider": "Provider",
	"individual-max":      "Max",
	"individual-ultra":    "Ultra",
	"teams-pro":           "Teams Pro",
}

type whoamiResponse struct {
	Success bool `json:"success"`
	User    struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		Email    string `json:"email"`
		UserName string `json:"userName"`
	} `json:"user"`
	Org *struct {
		ID    string `json:"id"`
		Login string `json:"login"`
		Name  string `json:"name"`
	} `json:"org"`
}

type creditsResponse struct {
	Credits struct {
		BelowThreshold   bool    `json:"belowThreshold"`
		CreditThreshold  float64 `json:"creditThreshold"`
		MonthlyCredits   float64 `json:"monthlyCredits"`
		PurchasedCredits float64 `json:"purchasedCredits"`
		FreeCredits      float64 `json:"freeCredits"`
	} `json:"credits"`
	WindowLimits   *windowLimits `json:"windowLimits"`
	SandboxAccess  bool          `json:"sandboxAccess"`
	SandboxMinutes *sandboxUsage `json:"sandboxMinutes"`
}

type sandboxUsage struct {
	LimitMinutes float64 `json:"limitMinutes"`
	UsedMinutes  float64 `json:"usedMinutes"`
}

type windowLimits struct {
	Limited  bool         `json:"limited"`
	FiveHour *windowLimit `json:"fiveHour"`
	Weekly   *windowLimit `json:"weekly"`
}

type windowLimit struct {
	Used     float64 `json:"used"`
	Cap      float64 `json:"cap"`
	Exceeded bool    `json:"exceeded"`
	ResetAt  int64   `json:"resetAt"` // epoch milliseconds
}

type subscriptionResponse struct {
	Success bool `json:"success"`
	Data    *struct {
		ID                 string `json:"id"`
		Status             string `json:"status"`
		PlanID             string `json:"planId"`
		CurrentPeriodStart string `json:"currentPeriodStart"`
		CurrentPeriodEnd   string `json:"currentPeriodEnd"`
		CancelAtPeriodEnd  bool   `json:"cancelAtPeriodEnd"`
	} `json:"data"`
}

type summaryResponse struct {
	TotalCount     float64 `json:"totalCount"`
	TotalCost      float64 `json:"totalCost"`
	AverageCost    float64 `json:"averageCost"`
	TotalTokensIn  float64 `json:"totalTokensIn"`
	TotalTokensOut float64 `json:"totalTokensOut"`
	TotalTokens    float64 `json:"totalTokens"`
	TotalCredits   float64 `json:"totalCredits"`
	PeriodBasis    string  `json:"periodBasis"`
}

type authFile struct {
	APIKey string `json:"apiKey"`
}

// Provider reports Command Code credit balance, usage windows and spend.
type Provider struct {
	providerbase.Base
}

// New constructs the Command Code provider with its dashboard widget.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Command Code",
				Capabilities: []string{"credits_endpoint", "usage_endpoint", "limits_endpoint", "plan_endpoint", "local_auth_file"},
				DocURL:       "https://commandcode.ai/docs/provider",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        EnvAPIKey,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set COMMAND_CODE_API_KEY, or sign in with the Command Code CLI (`cmd login`) so ~/.commandcode/auth.json can be reused.",
					"openusage auto-detects ~/.commandcode/ when the CLI has been used.",
				},
			},
			Dashboard: dashboardWidget(),
			CreditMetrics: map[string]core.BalanceSemantics{
				"credit_balance":  core.BalancePoint,
				"extra_credits":   core.BalancePoint,
				"monthly_spend":   core.BalanceCumulative,
				"usage_five_hour": core.BalanceLimit,
				"usage_seven_day": core.BalanceLimit,
			},
		}),
	}
}

// Fetch reads the Command Code billing/usage surface for one account.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, source := resolveAPIKey(acct)
	if apiKey == "" {
		return core.NewAuthSnapshot(ID, acct.ID,
			"no API key (set COMMAND_CODE_API_KEY or sign in with `cmd login`)"), nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(ID, acct.ID)

	orgID := ""
	whoami, status, err := p.fetchWhoami(ctx, baseURL, apiKey)
	switch {
	case err == nil:
		applyWhoami(whoami, &snap)
		if whoami.Org != nil {
			orgID = whoami.Org.ID
		}
	case isAuthStatus(status):
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", status)
		return snap, nil
	default:
		snap.SetDiagnostic("whoami_error", err.Error())
	}

	planID, err := p.fetchSubscription(ctx, baseURL, apiKey, orgID, &snap)
	if err != nil {
		snap.SetDiagnostic("subscription_error", err.Error())
	}

	status, err = p.fetchCredits(ctx, baseURL, apiKey, orgID, planID, &snap)
	if err != nil {
		if isAuthStatus(status) {
			snap.Status = core.StatusAuth
			snap.Message = fmt.Sprintf("HTTP %d – check API key", status)
			return snap, nil
		}
		if status == http.StatusTooManyRequests {
			snap.Status = core.StatusLimited
			snap.Message = "rate limited (HTTP 429)"
			return snap, nil
		}
		return snap, fmt.Errorf("command_code: credits: %w", err)
	}

	if err := p.fetchSummaries(ctx, baseURL, apiKey, orgID, &snap); err != nil {
		snap.SetDiagnostic("summary_error", err.Error())
	}

	snap.SetAttribute("auth_type", "api_key")
	if source == "auth_file" {
		snap.SetAttribute("credential_source", "~/.commandcode/auth.json")
	}

	shared.FinalizeStatus(&snap)
	return snap, nil
}

func (p *Provider) fetchWhoami(ctx context.Context, baseURL, apiKey string) (whoamiResponse, int, error) {
	var out whoamiResponse
	url := baseURL + whoamiPath + "?limits=1"
	status, _, err := shared.FetchJSON(ctx, url, apiKey, &out, p.Client())
	return out, status, err
}

func (p *Provider) fetchSubscription(ctx context.Context, baseURL, apiKey, orgID string, snap *core.UsageSnapshot) (string, error) {
	var out subscriptionResponse
	status, _, err := shared.FetchJSON(ctx, withOrg(baseURL+subscriptionPath, orgID), apiKey, &out, p.Client())
	if err != nil {
		return "", fmt.Errorf("subscription: %w (HTTP %d)", err, status)
	}
	if out.Data == nil {
		return "", nil
	}

	planID := out.Data.PlanID
	if name := planName(planID); name != "" {
		snap.SetAttribute("plan_name", name)
	}
	snap.SetAttribute("plan_id", planID)
	snap.SetAttribute("subscription_status", out.Data.Status)
	if out.Data.CurrentPeriodStart != "" {
		snap.SetAttribute("billing_cycle_start", out.Data.CurrentPeriodStart)
	}
	if out.Data.CurrentPeriodEnd != "" {
		snap.SetAttribute("billing_cycle_end", out.Data.CurrentPeriodEnd)
	}
	return planID, nil
}

func (p *Provider) fetchCredits(ctx context.Context, baseURL, apiKey, orgID, planID string, snap *core.UsageSnapshot) (int, error) {
	var out creditsResponse
	status, _, err := shared.FetchJSON(ctx, withOrg(baseURL+creditsPath, orgID), apiKey, &out, p.Client())
	if err != nil {
		return status, fmt.Errorf("credits: %w", err)
	}

	monthly := max(0, out.Credits.MonthlyCredits)
	purchased := max(0, out.Credits.PurchasedCredits)
	free := max(0, out.Credits.FreeCredits)
	extra := purchased + free
	remaining := monthly + extra

	balance := core.Metric{Remaining: core.Float64Ptr(remaining), Unit: "USD", Window: "current"}
	pool := float64(0)
	if total := planCredits[planID]; total > 0 {
		pool = total + extra
	}
	if pool > 0 {
		balance.Limit = core.Float64Ptr(pool)
	}
	snap.Metrics["credit_balance"] = balance

	if extra > 0 {
		snap.Metrics["extra_credits"] = core.Metric{Remaining: core.Float64Ptr(extra), Unit: "USD", Window: "current"}
	}

	// Plan usage is the included monthly credits consumed; purchased/free extra
	// credits don't count against the plan.
	if total := planCredits[planID]; total > 0 {
		usedPct := max(0, min(100, (total-monthly)/total*100))
		snap.Metrics["plan_percent_used"] = core.Metric{Used: core.Float64Ptr(usedPct), Limit: core.Float64Ptr(100), Unit: "%", Window: "billing-cycle"}
	}

	snap.Raw["credit_monthly_remaining"] = trimFloat(monthly)
	snap.Raw["credit_purchased_remaining"] = trimFloat(purchased)
	snap.Raw["credit_free_remaining"] = trimFloat(free)

	if w := out.WindowLimits; w != nil {
		snap.Raw["window_limits_limited"] = strconv.FormatBool(w.Limited)
		applyWindow(snap, "usage_five_hour", "5h", w.FiveHour)
		applyWindow(snap, "usage_seven_day", "7d", w.Weekly)

		var exceeded *windowLimit
		switch {
		case w.FiveHour != nil && w.FiveHour.Exceeded:
			exceeded = w.FiveHour
			snap.Message = "5-hour usage limit reached"
		case w.Weekly != nil && w.Weekly.Exceeded:
			exceeded = w.Weekly
			snap.Message = "weekly usage limit reached"
		}
		if exceeded != nil {
			snap.Status = core.StatusLimited
			if exceeded.ResetAt > 0 {
				snap.Message += fmt.Sprintf(" (resets %s)", time.UnixMilli(exceeded.ResetAt).Local().Format("15:04"))
			}
		}
	}

	return status, nil
}

func applyWindow(snap *core.UsageSnapshot, key, window string, w *windowLimit) {
	if w == nil || w.Cap <= 0 {
		return
	}
	used := w.Used
	resetAt := time.Time{}
	if w.ResetAt > 0 {
		resetAt = time.UnixMilli(w.ResetAt)
		if !resetAt.After(time.Now()) {
			used = 0
		}
		snap.Resets[key] = resetAt
	}
	usedPct := max(0, min(100, used/w.Cap*100))
	snap.Metrics[key] = core.Metric{
		Used:   core.Float64Ptr(usedPct),
		Limit:  core.Float64Ptr(100),
		Unit:   "%",
		Window: window,
	}
	snap.Raw[key+"_used_usd"] = trimFloat(w.Used)
	snap.Raw[key+"_cap_usd"] = trimFloat(w.Cap)
}

// fetchSummaries records billing-period spend/tokens and today's spend.
func (p *Provider) fetchSummaries(ctx context.Context, baseURL, apiKey, orgID string, snap *core.UsageSnapshot) error {
	period, status, err := p.fetchSummary(ctx, baseURL, apiKey, orgID, "")
	if err != nil {
		return fmt.Errorf("summary: %w (HTTP %d)", err, status)
	}
	snap.Metrics["monthly_spend"] = core.Metric{Used: core.Float64Ptr(period.TotalCost), Unit: "USD", Window: "billing-cycle"}
	snap.Metrics["total_tokens"] = core.Metric{Used: core.Float64Ptr(period.TotalTokens), Unit: "tokens", Window: "billing-cycle"}
	snap.Metrics["input_tokens"] = core.Metric{Used: core.Float64Ptr(period.TotalTokensIn), Unit: "tokens", Window: "billing-cycle"}
	snap.Metrics["output_tokens"] = core.Metric{Used: core.Float64Ptr(period.TotalTokensOut), Unit: "tokens", Window: "billing-cycle"}
	snap.Metrics["requests"] = core.Metric{Used: core.Float64Ptr(period.TotalCount), Unit: "requests", Window: "billing-cycle"}

	today, status, err := p.fetchSummary(ctx, baseURL, apiKey, orgID, startOfTodayRFC3339())
	if err != nil {
		// Non-fatal: keep the billing-period picture.
		snap.SetDiagnostic("today_summary_error", fmt.Sprintf("%v (HTTP %d)", err, status))
		return nil
	}
	snap.Metrics["today_spend"] = core.Metric{Used: core.Float64Ptr(today.TotalCost), Unit: "USD", Window: "today"}
	snap.Metrics["requests_today"] = core.Metric{Used: core.Float64Ptr(today.TotalCount), Unit: "requests", Window: "today"}
	return nil
}

func (p *Provider) fetchSummary(ctx context.Context, baseURL, apiKey, orgID, since string) (summaryResponse, int, error) {
	var out summaryResponse
	q := url.Values{}
	if orgID != "" {
		q.Set("orgId", orgID)
	}
	if since != "" {
		q.Set("since", since)
	}
	u := baseURL + summaryPath
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}
	status, _, err := shared.FetchJSON(ctx, u, apiKey, &out, p.Client())
	return out, status, err
}

func applyWhoami(w whoamiResponse, snap *core.UsageSnapshot) {
	snap.SetAttribute("account_email", w.User.Email)
	snap.SetAttribute("account_name", w.User.UserName)
	if w.User.Name != "" {
		snap.SetAttribute("account_display_name", w.User.Name)
	}
	if w.Org != nil {
		if w.Org.Name != "" {
			snap.SetAttribute("organization_name", w.Org.Name)
		} else if w.Org.Login != "" {
			snap.SetAttribute("organization_name", w.Org.Login)
		}
	}
}

// resolveAPIKey returns the credential and a source label ("env" or
// "auth_file"). Configured token / COMMAND_CODE_API_KEY wins; the CLI's
// ~/.commandcode/auth.json is the zero-config fallback.
func resolveAPIKey(acct core.AccountConfig) (string, string) {
	if key := strings.TrimSpace(acct.ResolveAPIKey()); key != "" {
		return key, "env"
	}
	if key := readAuthFileKey(acct); key != "" {
		return key, "auth_file"
	}
	return "", ""
}

func readAuthFileKey(acct core.AccountConfig) string {
	path := acct.Path("auth_file", defaultAuthFilePath())
	if path == "" {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var a authFile
	if err := json.Unmarshal(data, &a); err != nil {
		return ""
	}
	return strings.TrimSpace(a.APIKey)
}

func defaultAuthFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".commandcode", "auth.json")
}

func withOrg(url, orgID string) string {
	if orgID == "" {
		return url
	}
	return url + "?orgId=" + orgID
}

func planName(planID string) string {
	if name, ok := planNames[planID]; ok {
		return name
	}
	return ""
}

func isAuthStatus(code int) bool {
	return code == http.StatusUnauthorized || code == http.StatusForbidden
}

// startOfTodayRFC3339 returns local midnight as a UTC RFC3339 timestamp. The
// summary endpoint only accepts the "Z" form — RFC3339 offsets are rejected
// with HTTP 400.
func startOfTodayRFC3339() string {
	now := time.Now()
	midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return midnight.UTC().Format(time.RFC3339)
}

func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
