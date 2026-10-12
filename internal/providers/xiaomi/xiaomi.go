// Package xiaomi implements a usage provider for Xiaomi MiMo.
//
// MiMo's inference API (api.xiaomimimo.com) exposes no balance or usage
// endpoint for API keys. Wallet, spend and Token Plan quota live behind the
// web console API (platform.xiaomimimo.com/api/v1), which authenticates with
// a service token the console sets after Xiaomi account sign-in. That token is
// a session cookie, which browsers never write to their cookie store, so the
// provider reads the persistent Xiaomi account cookies (passToken, userId)
// from the user's browser and mints a console session from them through the
// console's own sign-in redirect chain (see mint.go). A Cookie header pasted
// into XIAOMI_MIMO_COOKIE bypasses the browser entirely.
package xiaomi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/browsercookies"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	consoleBaseURL = "https://platform.xiaomimimo.com/api/v1"
	consoleOrigin  = "https://platform.xiaomimimo.com"

	accountDomain      = "account.xiaomi.com"
	passTokenCookie    = "passToken"
	serviceTokenCookie = "api-platform_serviceToken"
	userIDCookie       = "userId"

	// cookieEnvVar holds a full Cookie header copied from the console, for
	// machines where no supported browser cookie store is readable.
	cookieEnvVar = "XIAOMI_MIMO_COOKIE"
	// baseURLEnvVar is a test seam; production never sets it.
	baseURLEnvVar = "OPENUSAGE_XIAOMI_CONSOLE_BASE_URL"

	monthTotalTokenItem = "month_total_token"
)

// loadSession and readAccountCookie are package-level seams so tests can
// drive Fetch without a real browser cookie store.
var (
	loadSession = func(ctx context.Context, acct core.AccountConfig) (config.BrowserSession, bool, error) {
		return shared.LoadOrRefreshBrowserSession(ctx, acct, nil)
	}
	readAccountCookie = func(ctx context.Context, name, browser string) string {
		cookie, err := browsercookies.New().ReadCookie(ctx, accountDomain, name, browser)
		if err != nil {
			return ""
		}
		return cookie.Value
	}
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "xiaomi",
			Info: core.ProviderInfo{
				Name:         "Xiaomi MiMo",
				Capabilities: []string{"console_billing", "console_usage_analytics", "token_plan_quota"},
				DocURL:       "https://mimo.mi.com/docs/en-US/api/guidance/rate-limit",
			},
			Auth: core.ProviderAuthSpec{
				Type:                core.ProviderAuthTypeBrowserSession,
				DefaultAccountID:    "xiaomi",
				BrowserCookieDomain: accountDomain,
				BrowserCookieName:   passTokenCookie,
				BrowserConsoleURL:   "https://platform.xiaomimimo.com/console/balance",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Log into https://platform.xiaomimimo.com/console/balance in your browser.",
					"In openusage: Settings → 5 KEYS → xiaomi → press Enter to import the Xiaomi account cookie.",
					"Or set XIAOMI_MIMO_COOKIE to the Cookie header of a console request.",
				},
			},
			Dashboard: dashboardWidget(),
			CreditMetrics: map[string]core.BalanceSemantics{
				"total_spend":   core.BalanceCumulative,
				"total_balance": core.BalancePoint,
			},
		}),
	}
}

// Fetch is console-session-auth only. Without a session it returns a clear
// AUTH snapshot pointing the user at the connect flow.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	sess, err := resolveSession(ctx, acct)
	if err != nil {
		return snap, fmt.Errorf("xiaomi: load session: %w", err)
	}
	if sess.cookieHeader == "" {
		snap.Status = core.StatusAuth
		snap.Message = sess.problem
		return snap, nil
	}
	snap.SetAttribute("auth_scope", "console_session")
	snap.SetAttribute("console_session_source", sess.source)

	client := newConsoleClient(sess.cookieHeader)

	var balance balanceData
	err = client.get(ctx, "/balance", &balance)
	if isAuthError(err) && sess.minted {
		// The cached console session expired; mint a new one and retry once.
		forgetMintedSession(acct.ID)
		if sess, err = resolveSession(ctx, acct); err != nil {
			return snap, fmt.Errorf("xiaomi: load session: %w", err)
		}
		if sess.cookieHeader == "" {
			snap.Status = core.StatusAuth
			snap.Message = sess.problem
			return snap, nil
		}
		client = newConsoleClient(sess.cookieHeader)
		err = client.get(ctx, "/balance", &balance)
	}
	if err != nil {
		return failedSnapshot(snap, err, "balance")
	}
	currency := normalizeCurrency(balance.Currency)
	snap.SetAttribute("currency", currency)
	if balance.Balance.set {
		snap.Metrics["total_balance"] = core.Metric{Remaining: core.Float64Ptr(balance.Balance.value), Unit: currency, Window: "current"}
	}
	if balance.CashBalance.set {
		snap.Metrics["cash_balance"] = core.Metric{Remaining: core.Float64Ptr(balance.CashBalance.value), Unit: currency, Window: "current"}
	}
	if balance.GiftBalance.set {
		snap.Metrics["gift_balance"] = core.Metric{Remaining: core.Float64Ptr(balance.GiftBalance.value), Unit: currency, Window: "current"}
	}

	// Spend and token totals are secondary: a failure here keeps the wallet
	// figures and records the error instead of failing the tile.
	var usage usageData
	if err := client.get(ctx, "/usage", &usage); err != nil {
		if isAuthError(err) {
			return failedSnapshot(snap, err, "usage")
		}
		snap.Raw["console_usage_error"] = err.Error()
	} else {
		applyUsage(&snap, usage, currency)
	}

	// Token Plan routes answer with a non-zero code for pay-as-you-go
	// accounts, so any failure short of an expired session is "no plan".
	var detail planDetailData
	if err := client.get(ctx, "/tokenPlan/detail", &detail); err == nil {
		applyPlanDetail(&snap, detail)
		var planUsage planUsageData
		if err := client.get(ctx, "/tokenPlan/usage", &planUsage); err == nil {
			applyPlanUsage(&snap, planUsage)
		}
	}

	snap.Status = core.StatusOK
	snap.Message = statusMessage(snap, currency)
	return snap, nil
}

const (
	problemNotConfigured = "Xiaomi account session not found — sign in at platform.xiaomimimo.com in your browser, or set " + cookieEnvVar
	problemMintFailed    = "could not open a console session — sign in again at platform.xiaomimimo.com"
)

// consoleSession is the Cookie header to send to the console and where it
// came from. An empty header carries the reason in problem.
type consoleSession struct {
	cookieHeader string
	source       string
	minted       bool
	problem      string
}

// resolveSession returns the console session to use. A pasted header in the
// environment wins; otherwise a session minted from the browser's Xiaomi
// account cookies is reused until the console rejects it.
func resolveSession(ctx context.Context, acct core.AccountConfig) (consoleSession, error) {
	if pasted := normalizeCookieHeader(os.Getenv(cookieEnvVar)); pasted != "" {
		return consoleSession{cookieHeader: pasted, source: "env"}, nil
	}
	if header, ok := cachedMintedSession(acct.ID); ok {
		return consoleSession{cookieHeader: header, source: "account_sso", minted: true}, nil
	}

	account, ok, err := loadSession(ctx, acct)
	if err != nil {
		return consoleSession{}, err
	}
	if !ok || strings.TrimSpace(account.Value) == "" {
		return consoleSession{problem: problemNotConfigured}, nil
	}
	if !mintAllowed(acct.ID) {
		return consoleSession{problem: problemMintFailed}, nil
	}

	accountCookies := passTokenCookie + "=" + account.Value
	for _, name := range []string{userIDCookie, "cUserId", "deviceId"} {
		if value := strings.TrimSpace(readAccountCookie(ctx, name, account.SourceBrowser)); value != "" {
			accountCookies += "; " + name + "=" + value
		}
	}

	header, err := mintConsoleSession(ctx, accountCookies)
	if err != nil {
		recordMintFailure(acct.ID)
		return consoleSession{problem: problemMintFailed}, nil
	}
	storeMintedSession(acct.ID, header)
	return consoleSession{cookieHeader: header, source: "account_sso", minted: true}, nil
}

// normalizeCookieHeader accepts a raw header value or a full "Cookie: …"
// line as copied from browser dev tools.
func normalizeCookieHeader(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "cookie:") {
		raw = strings.TrimSpace(raw[7:])
	}
	return raw
}

func failedSnapshot(snap core.UsageSnapshot, err error, endpoint string) (core.UsageSnapshot, error) {
	var limited *consoleRateLimitError
	switch {
	case isAuthError(err):
		snap.Status = core.StatusAuth
		snap.Message = "session expired — sign in again at platform.xiaomimimo.com"
		return snap, nil
	case errors.As(err, &limited):
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
		return snap, nil
	}
	return snap, fmt.Errorf("xiaomi: %s: %w", endpoint, err)
}

func applyUsage(snap *core.UsageSnapshot, usage usageData, currency string) {
	if usage.CostUsage.TotalCost.set {
		snap.Metrics["total_spend"] = core.Metric{Used: core.Float64Ptr(usage.CostUsage.TotalCost.value), Unit: currency, Window: "all-time"}
	}
	if usage.CostUsage.CurrentMonthCost.set {
		snap.Metrics["month_spend"] = core.Metric{Used: core.Float64Ptr(usage.CostUsage.CurrentMonthCost.value), Unit: currency, Window: "month"}
	}
	tokens := usage.TokenUsage
	setTokens := func(key string, n flexNumber) {
		if n.set {
			snap.Metrics[key] = core.Metric{Used: core.Float64Ptr(n.value), Unit: "tokens", Window: "all-time"}
		}
	}
	setTokens("total_tokens", tokens.TotalToken)
	setTokens("input_tokens", tokens.InputToken)
	setTokens("output_tokens", tokens.OutputToken)
	setTokens("cache_tokens", tokens.CacheToken)
	if usage.PluginUsage.TotalRequestCount.set {
		snap.Metrics["plugin_requests"] = core.Metric{Used: core.Float64Ptr(usage.PluginUsage.TotalRequestCount.value), Unit: "requests", Window: "all-time"}
	}
}

func applyPlanDetail(snap *core.UsageSnapshot, detail planDetailData) {
	name := core.FirstNonEmpty(strings.TrimSpace(detail.PlanName), strings.TrimSpace(detail.PlanCode))
	if name == "" {
		return
	}
	snap.SetAttribute("plan_name", name)
	snap.SetAttribute("plan_expired", strconv.FormatBool(detail.Expired))
}

func applyPlanUsage(snap *core.UsageSnapshot, usage planUsageData) {
	for _, item := range usage.MonthUsage.Items {
		if item.Name != monthTotalTokenItem || !item.Limit.set || item.Limit.value <= 0 {
			continue
		}
		used := item.Used.value
		snap.Metrics["plan_tokens"] = core.Metric{
			Used:      core.Float64Ptr(used),
			Limit:     core.Float64Ptr(item.Limit.value),
			Remaining: core.Float64Ptr(max(item.Limit.value-used, 0)),
			Unit:      "tokens",
			Window:    "month",
		}
		return
	}
}

func statusMessage(snap core.UsageSnapshot, currency string) string {
	if m, ok := snap.Metrics["total_balance"]; ok && m.Remaining != nil {
		return fmt.Sprintf("Balance: %.2f %s", *m.Remaining, currency)
	}
	return "OK"
}

func normalizeCurrency(raw string) string {
	raw = strings.ToUpper(strings.TrimSpace(raw))
	if len(raw) != 3 {
		return "USD"
	}
	return raw
}

// flexNumber decodes a JSON number or a numeric string; the console returns
// money as either depending on the field.
type flexNumber struct {
	value float64
	set   bool
}

func (n *flexNumber) UnmarshalJSON(data []byte) error {
	s := strings.Trim(strings.TrimSpace(string(data)), `"`)
	if s == "" || s == "null" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return nil
	}
	n.value = v
	n.set = true
	return nil
}

type balanceData struct {
	Balance     flexNumber `json:"balance"`
	CashBalance flexNumber `json:"cashBalance"`
	GiftBalance flexNumber `json:"giftBalance"`
	Currency    string     `json:"currency"`
}

type usageData struct {
	CostUsage struct {
		TotalCost        flexNumber `json:"totalCost"`
		CurrentMonthCost flexNumber `json:"currentMonthCost"`
	} `json:"costUsage"`
	TokenUsage struct {
		TotalToken  flexNumber `json:"totalToken"`
		InputToken  flexNumber `json:"inputToken"`
		OutputToken flexNumber `json:"outputToken"`
		CacheToken  flexNumber `json:"cacheToken"`
	} `json:"tokenUsage"`
	PluginUsage struct {
		TotalRequestCount flexNumber `json:"totalRequestCount"`
	} `json:"pluginUsage"`
}

type planDetailData struct {
	PlanName string `json:"planName"`
	PlanCode string `json:"planCode"`
	Expired  bool   `json:"expired"`
}

type planUsageData struct {
	MonthUsage struct {
		Items []struct {
			Name  string     `json:"name"`
			Used  flexNumber `json:"used"`
			Limit flexNumber `json:"limit"`
		} `json:"items"`
	} `json:"monthUsage"`
}

type consoleClient struct {
	httpClient   *http.Client
	baseURL      string
	cookieHeader string
}

func consoleBase() string {
	if override := strings.TrimSpace(os.Getenv(baseURLEnvVar)); override != "" {
		return strings.TrimRight(override, "/")
	}
	return consoleBaseURL
}

func newConsoleClient(cookieHeader string) *consoleClient {
	return &consoleClient{
		httpClient:   &http.Client{Timeout: 15 * time.Second},
		baseURL:      consoleBase(),
		cookieHeader: cookieHeader,
	}
}

// get fetches one console route and decodes the `data` member of its
// {code, message, data} envelope into out.
func (c *consoleClient) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Cookie", c.cookieHeader)
	req.Header.Set("Origin", consoleOrigin)
	req.Header.Set("Referer", consoleOrigin+"/console/balance")
	req.Header.Set("User-Agent", "openusage/xiaomi-console")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("xiaomi console: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return &consoleAuthError{Code: resp.StatusCode}
	case resp.StatusCode == http.StatusTooManyRequests:
		return &consoleRateLimitError{}
	case resp.StatusCode >= 400:
		return fmt.Errorf("xiaomi console: HTTP %d on %s", resp.StatusCode, path)
	}

	var envelope struct {
		Code     int             `json:"code"`
		Message  string          `json:"message"`
		LoginURL string          `json:"loginUrl"`
		Data     json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("xiaomi console: decode %s: %w", path, err)
	}
	if envelope.Code == http.StatusUnauthorized || envelope.Code == http.StatusForbidden || envelope.LoginURL != "" {
		return &consoleAuthError{Code: envelope.Code}
	}
	if envelope.Code != 0 {
		return fmt.Errorf("xiaomi console: %s returned code %d (%s)", path, envelope.Code, shorten(envelope.Message))
	}
	if out == nil || len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return nil
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("xiaomi console: decode %s data: %w", path, err)
	}
	return nil
}

type consoleAuthError struct {
	Code int
}

func (e *consoleAuthError) Error() string {
	return fmt.Sprintf("xiaomi console auth failed (code %d)", e.Code)
}

type consoleRateLimitError struct{}

func (e *consoleRateLimitError) Error() string { return "xiaomi console rate limited (HTTP 429)" }

func isAuthError(err error) bool {
	var aerr *consoleAuthError
	return errors.As(err, &aerr)
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}
