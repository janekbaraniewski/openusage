package opencode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// OpenCode console exposes data behind SolidStart server functions reachable
// at https://opencode.ai/_server. Each function has a content-hash ID
// (sha256 of its server-side source); these IDs change on every backend
// deploy. Pinned IDs below were captured 2026-04-30 from the user's HAR
// and cross-referenced against CodexBar's captures.
// The IDs are paired with a stable "purpose" name so we can grep / replace
// them in one place when they rotate.
const (
	consoleBaseURL = "https://opencode.ai"

	// workspaces — returns the user's workspace list (each containing an
	// id field like "wrk_..."). No args required.
	rpcWorkspacesID = "def39973159c7f0483d8793a822b8dbb10d067e12c65455fcb4608459ba0234f"

	// subscription.get — returns rolling 5-hour and weekly usage
	// percentages with reset timers. Args: [workspaceID].
	rpcSubscriptionID = "7abeebee372f304e050aaaf92be863f4a86490e382f8c79db68fd94040d691b4"

	// queryBillingInfo — returns balance, monthly limit, monthly usage,
	// auto-reload config, payment method, subscription state.
	// Args: [workspaceID].
	rpcBillingInfoID = "c83b78a614689c38ebee981f9b39a8b377716db85c1fd7dbab604adc02d3313d"

	// queryKeys — returns the workspace's API keys with timeUsed,
	// keyDisplay, name. Args: [workspaceID].
	rpcKeysID = "c22cd964237ba79f2f9b95faa2a14b804f870d1bab49279463379cc6a0fd0c85"

	// queryUsage — returns recent usage records (per-call entries with
	// model, tokens, cost). Args: [workspaceID, offset].
	rpcUsageID = "bfd684bfc2e4eed05cd0b518f5e4eafd3f3376e3938abb9e536e7c03df831e5c"

	// queryUsageMonth (POST) — returns daily usage roll-up + key list for
	// a year/month. Args: [workspaceID, year, month, tz].
	rpcUsageMonthID = "15702f3a12ff8bff357f8c2aa154a17e65b746d5f6b96adc9002c86ee0c15205"
)

var workspaceRedirectRE = regexp.MustCompile(`/workspace/([^/?#]+)`)

// ConsoleClient is a minimal SolidStart RPC client for the OpenCode console.
// Cookie-authed; never writes mutations.
type ConsoleClient struct {
	httpClient *http.Client
	baseURL    string

	// Cookie is the session cookie value (typically the `auth` cookie's
	// content). The runtime composes a Cookie header from this on every
	// request — it's a credential, never logged.
	Cookie     string
	CookieName string

	// WorkspaceID identifies which OpenCode workspace to query. Required
	// for billing.get, queryKeys, etc. — without it we'd query the empty
	// "default" which most of the RPCs reject.
	WorkspaceID string
}

// NewConsoleClient returns a client with sane defaults: 15s HTTP timeout,
// pointing at https://opencode.ai. Tests can override baseURL.
func NewConsoleClient(cookieValue, cookieName, workspaceID string) *ConsoleClient {
	return &ConsoleClient{
		httpClient:  &http.Client{Timeout: 15 * time.Second},
		baseURL:     consoleBaseURL,
		Cookie:      cookieValue,
		CookieName:  cookieName,
		WorkspaceID: workspaceID,
	}
}

// SerovalArg matches the JSON shape SolidStart's call serialisation uses.
// Each argument is a tiny tagged-union: `{t: 1, s: "<string>"}` for a
// string, `{t: 0, s: <number>}` for a number. Arrays of args wrap into
// `{t: 9, i: 0, l: <count>, a: [...args], o: 0}`.
type serovalArg struct {
	T int `json:"t"`
	S any `json:"s,omitempty"`
}

type serovalCall struct {
	T int          `json:"t"`
	I int          `json:"i"`
	L int          `json:"l"`
	A []serovalArg `json:"a"`
	O int          `json:"o"`
}

type serovalRequest struct {
	T serovalCall `json:"t"`
	F int         `json:"f"`
	M []any       `json:"m"`
}

// buildArgsPayload constructs the SolidStart args envelope. Mirrors what
// the browser sends — verified against captured HAR requests.
func buildArgsPayload(args ...any) serovalRequest {
	encoded := make([]serovalArg, 0, len(args))
	for _, a := range args {
		switch v := a.(type) {
		case string:
			encoded = append(encoded, serovalArg{T: 1, S: v})
		case int:
			encoded = append(encoded, serovalArg{T: 0, S: v})
		case float64:
			encoded = append(encoded, serovalArg{T: 0, S: v})
		default:
			// Fallback — treat anything else as a string. SolidStart
			// rejects unexpected shapes anyway, so this just forwards
			// the error rather than masking it.
			encoded = append(encoded, serovalArg{T: 1, S: fmt.Sprintf("%v", v)})
		}
	}
	return serovalRequest{
		T: serovalCall{T: 9, I: 0, L: len(args), A: encoded, O: 0},
		F: 31,
		M: []any{},
	}
}

// callGET invokes a GET-style server function (queryBillingInfo, queryKeys,
// queryUsage). The args payload is URL-encoded into the `args` query
// parameter; the function ID goes in both the `id` query param and the
// `x-server-id` header (browser sends both; the server checks one of them).
func (c *ConsoleClient) callGET(ctx context.Context, fnID string, args ...any) ([]byte, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return nil, errors.New("console: missing session cookie")
	}
	payload := buildArgsPayload(args...)
	argsJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("console: encode args: %w", err)
	}

	u := fmt.Sprintf("%s/_server?id=%s&args=%s", c.baseURL, fnID, url.QueryEscape(string(argsJSON)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req, fnID)

	return c.do(req)
}

// callPOST invokes a POST-style action (queryUsageMonth). The args payload
// is JSON-encoded as the request body; ID goes in the `x-server-id` header.
func (c *ConsoleClient) callPOST(ctx context.Context, fnID string, args ...any) ([]byte, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return nil, errors.New("console: missing session cookie")
	}
	payload := buildArgsPayload(args...)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("console: encode args: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/_server", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyHeaders(req, fnID)
	return c.do(req)
}

func (c *ConsoleClient) applyHeaders(req *http.Request, fnID string) {
	req.Header.Set("Accept", "*/*")
	if fnID != "" {
		req.Header.Set("x-server-id", fnID)
		req.Header.Set("x-server-instance", "openusage")
	}
	// Cookie header — single cookie, not a full jar. The session cookie
	// is the only one we need; OpenCode's console doesn't gate on
	// CSRF/anti-forgery for these GETs.
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: c.Cookie})
	req.Header.Set("User-Agent", "openusage/console-client")
}

// DiscoverWorkspaceID resolves the user's last-seen workspace by following the
// same authenticated redirect the OpenCode console uses for `/auth`.
func (c *ConsoleClient) DiscoverWorkspaceID(ctx context.Context) (string, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return "", errors.New("console: missing session cookie")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/auth", nil)
	if err != nil {
		return "", fmt.Errorf("console: auth redirect request: %w", err)
	}
	c.applyHeaders(req, "")

	client := *c.httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("console: auth redirect request: %w", err)
	}
	defer resp.Body.Close()

	location := resp.Header.Get("Location")
	if location == "" {
		if resp.Request != nil && resp.Request.URL != nil {
			location = resp.Request.URL.String()
		}
	}
	matches := workspaceRedirectRE.FindStringSubmatch(location)
	if len(matches) == 2 && strings.TrimSpace(matches[1]) != "" {
		return matches[1], nil
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", &ConsoleAuthError{StatusCode: resp.StatusCode, Body: "workspace discovery unauthorized"}
	}
	if location != "" {
		return "", fmt.Errorf("console: workspace redirect missing id (%s)", location)
	}
	return "", fmt.Errorf("console: workspace redirect missing id (HTTP %d)", resp.StatusCode)
}

// FetchWorkspaceIDsViaRPC discovers workspace IDs by calling the workspaces
// server function. This is more reliable than the /auth redirect approach
// used by DiscoverWorkspaceID. Returns the first workspace ID found, or
// an error if none could be parsed.
func (c *ConsoleClient) FetchWorkspaceIDsViaRPC(ctx context.Context) (string, error) {
	if c.Cookie == "" || c.CookieName == "" {
		return "", errors.New("console: missing session cookie")
	}

	// Try GET first, fall back to POST if empty.
	body, err := c.callGET(ctx, rpcWorkspacesID)
	if err != nil {
		return "", fmt.Errorf("console: workspaces GET: %w", err)
	}
	ids := parseWorkspaceIDsFromResponse(body)
	if len(ids) > 0 {
		return ids[0], nil
	}

	// POST fallback — some SolidStart deployments only honour POST.
	body, err = c.callPOST(ctx, rpcWorkspacesID)
	if err != nil {
		return "", fmt.Errorf("console: workspaces POST: %w", err)
	}
	ids = parseWorkspaceIDsFromResponse(body)
	if len(ids) == 0 {
		return "", errors.New("console: no workspace IDs found in workspaces response")
	}
	return ids[0], nil
}

// parseWorkspaceIDsFromResponse extracts workspace IDs (wrk_...) from a
// server response. Tries JSON parsing first, then falls back to regex.
func parseWorkspaceIDsFromResponse(body []byte) []string {
	parsed, err := ParseSeroval(body)
	if err != nil {
		return parseWorkspaceIDsFromBody(string(body))
	}
	return collectWorkspaceIDsFromJSON(parsed)
}

// collectWorkspaceIDsFromJSON recursively walks a parsed JSON structure
// looking for strings matching the workspace ID pattern.
func collectWorkspaceIDsFromJSON(v any) []string {
	var ids []string
	switch val := v.(type) {
	case map[string]any:
		for _, child := range val {
			ids = append(ids, collectWorkspaceIDsFromJSON(child)...)
		}
	case []any:
		for _, child := range val {
			ids = append(ids, collectWorkspaceIDsFromJSON(child)...)
		}
	case string:
		if len(val) > 4 && strings.HasPrefix(val, "wrk_") {
			ids = append(ids, val)
		}
	}
	seen := make(map[string]bool, len(ids))
	unique := ids[:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			unique = append(unique, id)
		}
	}
	return unique
}

// parseWorkspaceIDsFromBody uses regex to find workspace IDs in raw text.
func parseWorkspaceIDsFromBody(body string) []string {
	re := regexp.MustCompile(`wrk_[A-Za-z0-9]+`)
	matches := re.FindAllString(body, -1)
	seen := make(map[string]bool, len(matches))
	unique := matches[:0]
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			unique = append(unique, m)
		}
	}
	return unique
}

// SubscriptionUsage is the parsed shape of a subscription.get response.
// Contains rolling 5-hour, weekly, and monthly usage percentages with reset timers.
//
// The *OK fields distinguish "field present in the response with a value of
// 0" from "field absent" — a usagePercent of 0 is a legitimate reading (the
// quota window just reset) and must not be treated the same as "not found".
type SubscriptionUsage struct {
	RollingUsagePct float64
	RollingUsageOK  bool
	RollingResetSec int
	WeeklyUsagePct  float64
	WeeklyUsageOK   bool
	WeeklyResetSec  int
	MonthlyUsagePct float64
	MonthlyUsageOK  bool
	MonthlyResetSec int
	RenewAt         string
}

// QuerySubscriptionUsage fetches the rolling 5-hour and weekly usage
// percentages for the given workspace. These represent OpenCode's
// rate-limit quota consumption, not dollar spend.
func (c *ConsoleClient) QuerySubscriptionUsage(ctx context.Context, workspaceID string) (SubscriptionUsage, error) {
	if workspaceID == "" {
		return SubscriptionUsage{}, errors.New("console: workspace ID required")
	}

	referer := fmt.Sprintf("%s/workspace/%s/billing", c.baseURL, workspaceID)
	payload := buildArgsPayload(workspaceID)
	argsJSON, err := json.Marshal(payload)
	if err != nil {
		return SubscriptionUsage{}, fmt.Errorf("console: encode args: %w", err)
	}

	u := fmt.Sprintf("%s/_server?id=%s&args=%s", c.baseURL, rpcSubscriptionID, url.QueryEscape(string(argsJSON)))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return SubscriptionUsage{}, err
	}
	c.applyHeaders(req, rpcSubscriptionID)
	req.Header.Set("Referer", referer)
	req.Header.Set("Accept", "text/javascript, application/json;q=0.9, */*;q=0.8")

	body, err := c.do(req)
	if err != nil {
		return SubscriptionUsage{}, fmt.Errorf("console: subscription usage: %w", err)
	}

	return parseSubscriptionUsage(body)
}

// parseSubscriptionUsage extracts rolling/weekly usage from a subscription.get
// response. The response may be Seroval-encoded or plain JSON.
func parseSubscriptionUsage(body []byte) (SubscriptionUsage, error) {
	parsed, err := ParseSeroval(body)
	if err != nil {
		// Try regex fallback on raw text (CodexBar-style parsing).
		return parseSubscriptionUsageFallback(string(body))
	}

	result := SubscriptionUsage{}
	m, ok := parsed.(map[string]any)
	if !ok {
		return SubscriptionUsage{}, fmt.Errorf("console: subscription response not an object: %T", parsed)
	}

	// Try nested "usage" key first.
	usageMap := m
	if nested, ok := m["usage"].(map[string]any); ok {
		usageMap = nested
	}

	// Parse rolling usage.
	if rolling, ok := usageMap["rollingUsage"].(map[string]any); ok {
		result.RollingUsagePct = floatFieldFromMap(rolling, "usagePercent")
		result.RollingResetSec = int(floatFieldFromMap(rolling, "resetInSec"))
	}

	// Parse weekly usage.
	if weekly, ok := usageMap["weeklyUsage"].(map[string]any); ok {
		result.WeeklyUsagePct = floatFieldFromMap(weekly, "usagePercent")
		result.WeeklyResetSec = int(floatFieldFromMap(weekly, "resetInSec"))
	}

	// Parse renewAt if present.
	if v, ok := m["renewAt"]; ok {
		switch val := v.(type) {
		case string:
			result.RenewAt = val
		case float64:
			result.RenewAt = fmt.Sprintf("%v", val)
		}
	}

	if result.RollingUsagePct == 0 && result.WeeklyUsagePct == 0 {
		return SubscriptionUsage{}, errors.New("console: subscription response missing usage fields")
	}
	return result, nil
}

// parseSubscriptionUsageFallback uses regex to extract usage data from raw
// text responses, matching CodexBar's fallback parsing strategy.
func parseSubscriptionUsageFallback(text string) (SubscriptionUsage, error) {
	result := SubscriptionUsage{}

	if pct := extractDoubleFromText(text, `rollingUsage[^}]*?usagePercent\s*:\s*([0-9]+(?:\.[0-9]+)?)`); pct != nil {
		result.RollingUsagePct = *pct
	}
	if sec := extractIntFromText(text, `rollingUsage[^}]*?resetInSec\s*:\s*([0-9]+)`); sec != nil {
		result.RollingResetSec = *sec
	}
	if pct := extractDoubleFromText(text, `weeklyUsage[^}]*?usagePercent\s*:\s*([0-9]+(?:\.[0-9]+)?)`); pct != nil {
		result.WeeklyUsagePct = *pct
	}
	if sec := extractIntFromText(text, `weeklyUsage[^}]*?resetInSec\s*:\s*([0-9]+)`); sec != nil {
		result.WeeklyResetSec = *sec
	}

	if result.RollingUsagePct == 0 && result.WeeklyUsagePct == 0 {
		return SubscriptionUsage{}, errors.New("console: subscription response missing usage fields")
	}
	return result, nil
}

func floatFieldFromMap(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

func extractDoubleFromText(text, pattern string) *float64 {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return nil
	}
	if f, err := strconv.ParseFloat(matches[1], 64); err == nil {
		return &f
	}
	return nil
}

func extractIntFromText(text, pattern string) *int {
	re := regexp.MustCompile(pattern)
	matches := re.FindStringSubmatch(text)
	if len(matches) < 2 {
		return nil
	}
	if i, err := strconv.Atoi(matches[1]); err == nil {
		return &i
	}
	return nil
}

func (c *ConsoleClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("console: request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("console: read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, &ConsoleAuthError{StatusCode: resp.StatusCode, Body: shortenBody(body)}
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("console: http %d: %s", resp.StatusCode, shortenBody(body))
	}
	return body, nil
}

// CallGETRaw is a debug helper that performs a GET-style server function call
// and returns the raw response body. Not for production use.
func (c *ConsoleClient) CallGETRaw(ctx context.Context, fnID string, args ...any) ([]byte, error) {
	return c.callGET(ctx, fnID, args...)
}

// FetchPageRaw fetches a page from the OpenCode console with cookie auth.
// Used for scraping the Go usage page HTML.
func (c *ConsoleClient) FetchPageRaw(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	c.applyHeaders(req, "")
	return c.do(req)
}

// FetchGoUsagePage fetches the OpenCode Go usage page and extracts billing
// and subscription usage data from the embedded Seroval script. This is more
// reliable than individual RPC calls whose IDs rotate on every deploy.
func (c *ConsoleClient) FetchGoUsagePage(ctx context.Context, workspaceID string) (SubscriptionUsage, BillingInfo, error) {
	if workspaceID == "" {
		return SubscriptionUsage{}, BillingInfo{}, errors.New("console: workspace ID required")
	}

	url := fmt.Sprintf("%s/workspace/%s/go", c.baseURL, workspaceID)
	body, err := c.FetchPageRaw(ctx, url)
	if err != nil {
		return SubscriptionUsage{}, BillingInfo{}, fmt.Errorf("console: fetch go page: %w", err)
	}

	html := string(body)
	if looksSignedOutFromHTML(html) {
		return SubscriptionUsage{}, BillingInfo{}, &ConsoleAuthError{StatusCode: 401, Body: "page indicates signed out"}
	}

	subscription, billing := parseGoUsagePageHTML(html)
	return subscription, billing, nil
}

// looksSignedOutFromHTML checks if an HTML response indicates the user is
// not authenticated.
func looksSignedOutFromHTML(html string) bool {
	lower := strings.ToLower(html)
	return strings.Contains(lower, "sign in") ||
		strings.Contains(lower, "login") ||
		strings.Contains(lower, "auth/authorize")
}

// parseGoUsagePageHTML extracts billing and subscription usage data from the
// embedded Seroval script in the OpenCode Go usage page HTML.
func parseGoUsagePageHTML(html string) (SubscriptionUsage, BillingInfo) {
	subscription := SubscriptionUsage{}
	billing := BillingInfo{}

	// Extract the main Seroval script blob
	scriptRE := regexp.MustCompile(`self\.\$R=self\.\$R\|\|\[\].*`)
	scriptMatch := scriptRE.FindString(html)
	if scriptMatch == "" {
		return subscription, billing
	}

	// Extract billing data by finding the billing.get assignment followed by
	// the billing fields object. The pattern is:
	// billing.get["wrk_..."]}=$R[N]=$R[M]($R[O]={...billing fields...})
	billingObjRE := regexp.MustCompile(`billing\.get\["[^"]*"\].*?\$R\[\d+\]=\{([^}]+)\}`)
	if matches := billingObjRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		billing = parseBillingFields(matches[1])
	}

	// Extract subscription usage. The pattern is:
	// rollingUsage:$R[N]={status:"ok",resetInSec:N,usagePercent:N},...
	// Note: field order varies; use independent extractions.
	rollingBlockRE := regexp.MustCompile(`rollingUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := rollingBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.RollingUsagePct = v
			subscription.RollingUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.RollingResetSec = int(v)
		}
	}

	weeklyBlockRE := regexp.MustCompile(`weeklyUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := weeklyBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.WeeklyUsagePct = v
			subscription.WeeklyUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.WeeklyResetSec = int(v)
		}
	}

	monthlyBlockRE := regexp.MustCompile(`monthlyUsage:\$R\[\d+\]=\{([^}]+)\}`)
	if matches := monthlyBlockRE.FindStringSubmatch(scriptMatch); len(matches) > 1 {
		block := matches[1]
		if v, ok := extractFloatFieldOK(block, "usagePercent"); ok {
			subscription.MonthlyUsagePct = v
			subscription.MonthlyUsageOK = true
		}
		if v := extractFloatField(block, "resetInSec"); v > 0 {
			subscription.MonthlyResetSec = int(v)
		}
	}

	return subscription, billing
}

// parseBillingFields extracts billing info from a Seroval object fragment
// like: customerID:"cus_xxx",balance:0,reloadAmount:20,...
func parseBillingFields(fields string) BillingInfo {
	b := BillingInfo{}
	b.CustomerID = extractStringField(fields, "customerID")
	b.PaymentMethodType = extractStringField(fields, "paymentMethodType")
	b.SubscriptionPlan = extractStringField(fields, "subscriptionPlan")

	if v := extractFloatField(fields, "balance"); v != 0 {
		b.Balance = v
	}
	if v := extractFloatField(fields, "reloadAmount"); v != 0 {
		b.ReloadAmount = v
	}
	if v := extractFloatField(fields, "reloadTrigger"); v != 0 {
		b.ReloadTrigger = v
	}

	if v := extractNullableFloat(fields, "monthlyLimit"); v != nil {
		b.MonthlyLimit = v
	}
	if v := extractNullableFloat(fields, "monthlyUsage"); v != nil {
		b.MonthlyUsage = *v
	}

	if strings.Contains(fields, "subscriptionID:") && !strings.Contains(fields, "subscriptionID:null") {
		b.HasSubscription = true
	}

	return b
}

// extractStringField pulls a string value from a Seroval object fragment.
// Handles: field:"value" and field:null
func extractStringField(fields, key string) string {
	re := regexp.MustCompile(key + `:"([^"]*)"`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		return matches[1]
	}
	return ""
}

// extractFloatFieldOK pulls a numeric value from a Seroval object fragment,
// reporting via ok whether the field was present at all — distinguishing
// "field:0" (found, value 0) from "field absent" (not found).
func extractFloatFieldOK(fields, key string) (float64, bool) {
	re := regexp.MustCompile(key + `:(-?[0-9]+(?:\.[0-9]+)?)`)
	matches := re.FindStringSubmatch(fields)
	if len(matches) < 2 {
		return 0, false
	}
	f, _ := strconv.ParseFloat(matches[1], 64)
	return f, true
}

// extractFloatField pulls a numeric value from a Seroval object fragment.
// Handles: field:123, field:0, field:1.5
func extractFloatField(fields, key string) float64 {
	re := regexp.MustCompile(key + `:(-?[0-9]+(?:\.[0-9]+)?)`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		f, _ := strconv.ParseFloat(matches[1], 64)
		return f
	}
	return 0
}

// extractNullableFloat pulls a nullable numeric value from a Seroval fragment.
// Returns nil for null, pointer to value otherwise.
func extractNullableFloat(fields, key string) *float64 {
	re := regexp.MustCompile(key + `:(null|-?[0-9]+(?:\.[0-9]+)?)`)
	if matches := re.FindStringSubmatch(fields); len(matches) > 1 {
		if matches[1] == "null" {
			return nil
		}
		f, _ := strconv.ParseFloat(matches[1], 64)
		return &f
	}
	return nil
}

func shortenBody(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

// ConsoleAuthError is returned when the OpenCode console rejects our cookie
// (401/403). Callers treat this as "session expired — user needs to re-login
// in the browser" and surface AUTH on the tile.
type ConsoleAuthError struct {
	StatusCode int
	Body       string
}

func (e *ConsoleAuthError) Error() string {
	return fmt.Sprintf("opencode console auth failed: HTTP %d (%s)", e.StatusCode, e.Body)
}

// BillingInfo is the parsed shape of a queryBillingInfo response. Field names
// mirror the wire format so the parser → struct mapping is mechanical.
type BillingInfo struct {
	CustomerID         string
	PaymentMethodID    string
	PaymentMethodType  string
	PaymentMethodLast4 string
	Balance            float64 // in cents per OpenCode's persistence (formatBalance divides by 1e8 in their UI)
	MonthlyLimit       *float64
	MonthlyUsage       float64
	ReloadAmount       float64
	ReloadTrigger      float64
	SubscriptionPlan   string
	HasSubscription    bool
}

// QueryBillingInfo returns the user's billing state. Does not trigger any
// mutation server-side; safe to poll.
func (c *ConsoleClient) QueryBillingInfo(ctx context.Context) (BillingInfo, error) {
	if c.WorkspaceID == "" {
		return BillingInfo{}, errors.New("console: workspace ID required")
	}
	body, err := c.callGET(ctx, rpcBillingInfoID, c.WorkspaceID)
	if err != nil {
		return BillingInfo{}, err
	}
	parsed, err := ParseSeroval(body)
	if err != nil {
		return BillingInfo{}, err
	}
	return billingInfoFromMap(parsed)
}

func billingInfoFromMap(parsed any) (BillingInfo, error) {
	m, ok := parsed.(map[string]any)
	if !ok {
		return BillingInfo{}, fmt.Errorf("console: billing response not an object: %T", parsed)
	}
	out := BillingInfo{}
	out.CustomerID = stringField(m, "customerID")
	out.PaymentMethodID = stringField(m, "paymentMethodID")
	out.PaymentMethodType = stringField(m, "paymentMethodType")
	out.PaymentMethodLast4 = stringField(m, "paymentMethodLast4")
	out.Balance = floatField(m, "balance")
	out.MonthlyUsage = floatField(m, "monthlyUsage")
	out.ReloadAmount = floatField(m, "reloadAmount")
	out.ReloadTrigger = floatField(m, "reloadTrigger")
	out.SubscriptionPlan = stringField(m, "subscriptionPlan")
	if v, ok := m["subscriptionID"]; ok && v != nil {
		out.HasSubscription = true
	}
	if v, ok := m["monthlyLimit"]; ok {
		if f, ok := v.(float64); ok {
			out.MonthlyLimit = &f
		}
	}
	return out, nil
}

// UsageRow is one entry in queryUsage's array — a single chat completion
// from OpenCode Zen with metadata.
type UsageRow struct {
	Model        string
	Provider     string
	InputTokens  float64
	OutputTokens float64
	CacheTokens  float64
	CostUSD      float64
	KeyID        string
	SessionID    string
	TimeCreated  string
}

// QueryUsage returns the most recent usage records (offset 0 = newest).
func (c *ConsoleClient) QueryUsage(ctx context.Context, offset int) ([]UsageRow, error) {
	if c.WorkspaceID == "" {
		return nil, errors.New("console: workspace ID required")
	}
	body, err := c.callGET(ctx, rpcUsageID, c.WorkspaceID, offset)
	if err != nil {
		return nil, err
	}
	parsed, err := ParseSeroval(body)
	if err != nil {
		return nil, err
	}
	arr, ok := parsed.([]any)
	if !ok {
		return nil, fmt.Errorf("console: usage response not an array: %T", parsed)
	}
	out := make([]UsageRow, 0, len(arr))
	for _, raw := range arr {
		m, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, UsageRow{
			Model:        stringField(m, "model"),
			Provider:     stringField(m, "provider"),
			InputTokens:  floatField(m, "inputTokens"),
			OutputTokens: floatField(m, "outputTokens"),
			CacheTokens:  floatField(m, "cacheReadTokens"),
			CostUSD:      floatField(m, "cost"),
			KeyID:        stringField(m, "keyID"),
			SessionID:    stringField(m, "sessionID"),
			TimeCreated:  stringField(m, "timeCreated"),
		})
	}
	return out, nil
}

// MonthUsage is the parsed shape of queryUsageMonth — daily roll-up of
// per-model spend within a year/month for the workspace.
type MonthUsage struct {
	Days []DayUsage
	Keys []KeyDescriptor
}

type DayUsage struct {
	Date      string
	Model     string
	TotalCost float64
	KeyID     string
	Plan      string
}

type KeyDescriptor struct {
	ID          string
	DisplayName string
	Deleted     bool
}

// QueryUsageMonth returns daily usage roll-up for a year/month. Year is
// e.g. 2026; month is 1-indexed (Jan=1). tz is an offset string like
// "+02:00" — pass time.Local's offset for sensible local roll-ups.
func (c *ConsoleClient) QueryUsageMonth(ctx context.Context, year, month int, tz string) (MonthUsage, error) {
	if c.WorkspaceID == "" {
		return MonthUsage{}, errors.New("console: workspace ID required")
	}
	body, err := c.callPOST(ctx, rpcUsageMonthID, c.WorkspaceID, year, month, tz)
	if err != nil {
		return MonthUsage{}, err
	}
	parsed, err := ParseSeroval(body)
	if err != nil {
		return MonthUsage{}, err
	}
	m, ok := parsed.(map[string]any)
	if !ok {
		return MonthUsage{}, fmt.Errorf("console: usage-month response not an object: %T", parsed)
	}
	out := MonthUsage{}
	if days, ok := m["usage"].([]any); ok {
		for _, raw := range days {
			d, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			out.Days = append(out.Days, DayUsage{
				Date:      stringField(d, "date"),
				Model:     stringField(d, "model"),
				TotalCost: floatField(d, "totalCost"),
				KeyID:     stringField(d, "keyId"),
				Plan:      stringField(d, "plan"),
			})
		}
	}
	if keys, ok := m["keys"].([]any); ok {
		for _, raw := range keys {
			k, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			out.Keys = append(out.Keys, KeyDescriptor{
				ID:          stringField(k, "id"),
				DisplayName: stringField(k, "displayName"),
				Deleted:     boolField(k, "deleted"),
			})
		}
	}
	return out, nil
}

// stringField pulls a string out of a parsed map, returning "" for nil /
// missing / non-string. Tolerant by design — OpenCode populates many
// fields as null on fresh accounts and we'd rather show empty than crash.
func stringField(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// floatField pulls a number out of a parsed map. Returns 0 for nil /
// missing / non-numeric. JSON-unmarshalled numbers always come back as
// float64.
func floatField(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// boolField — same shape as the others, for `deleted` / `is_*` fields.
func boolField(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}
