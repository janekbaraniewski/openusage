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
	"strings"
	"time"
)

// OpenCode console exposes data behind SolidStart server functions reachable
// at https://opencode.ai/_server. Each function has a content-hash ID
// (sha256 of its server-side source); these IDs change on every backend
// deploy. Pinned IDs below were captured 2026-04-30 from the user's HAR.
// The IDs are paired with a stable "purpose" name so we can grep / replace
// them in one place when they rotate.
const (
	consoleBaseURL = "https://opencode.ai"

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
	req.Header.Set("x-server-id", fnID)
	req.Header.Set("x-server-instance", "openusage")
	// Cookie header — single cookie, not a full jar. The session cookie
	// is the only one we need; OpenCode's console doesn't gate on
	// CSRF/anti-forgery for these GETs.
	req.AddCookie(&http.Cookie{Name: c.CookieName, Value: c.Cookie})
	req.Header.Set("User-Agent", "openusage/console-client")
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
