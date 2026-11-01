package shared

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

func CreateStandardRequest(ctx context.Context, baseURL, endpoint, apiKey string, headers map[string]string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	if _, hasAuth := headers["Authorization"]; !hasAuth {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	return req, nil
}

func ProcessStandardResponse(resp *http.Response, acct core.AccountConfig, providerID string) (core.UsageSnapshot, error) {
	snap := core.NewUsageSnapshot(providerID, acct.ID)
	snap.Raw = parsers.RedactHeaders(resp.Header)
	ApplyStatusFromResponse(resp, &snap)
	return snap, nil
}

// ApplyStatusFromResponse sets snap.Status and snap.Message based on the HTTP
// status code. Centralises the 401/403 → StatusAuth, 429 → StatusLimited
// mapping that providers with custom response handling (mistral, gemini_api,
// alibaba_cloud, moonshot, zai) used to hand-roll. Call this first, then add
// provider-specific cases on top if needed. A 5xx maps to StatusError when no
// status was set yet. On 429 and 5xx, Retry-After is parsed into
// snap.NextPollNotBefore so the poller holds off until then.
func ApplyStatusFromResponse(resp *http.Response, snap *core.UsageSnapshot) {
	ApplyStatusFromCode(resp.StatusCode, snap, "")
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 && snap.Status == "" {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("upstream error (HTTP %d)", resp.StatusCode)
	}
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		ApplyRetryAfter(resp.Header, snap)
	}
}

// ApplyRetryAfter copies a Retry-After header into snap.Raw and
// snap.NextPollNotBefore. Providers with custom 429/5xx handling should call
// it so the daemon poller honours the server's requested delay.
func ApplyRetryAfter(h http.Header, snap *core.UsageSnapshot) {
	retryAfter := h.Get("Retry-After")
	if retryAfter == "" {
		return
	}
	if snap.Raw == nil {
		snap.Raw = make(map[string]string)
	}
	snap.Raw["retry_after"] = retryAfter
	if until, ok := parsers.ParseRetryAfter(retryAfter, time.Now()); ok && until.After(snap.NextPollNotBefore) {
		snap.NextPollNotBefore = until
	}
}

// ApplyStatusFromCode is the response-less variant for callers that only have
// the status code (e.g. shared.FetchJSON returns an error + status code).
// The keyHint is included in the auth-failure message — pass the env-var name
// the user should check (e.g. "MOONSHOT_API_KEY"). Empty means "API key".
func ApplyStatusFromCode(statusCode int, snap *core.UsageSnapshot, keyHint string) {
	switch statusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		hint := "API key"
		if keyHint != "" {
			hint = keyHint
		}
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check %s", statusCode, hint)
	case http.StatusTooManyRequests:
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
	}
}

func ApplyStandardRateLimits(resp *http.Response, snap *core.UsageSnapshot) {
	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")
}

func FinalizeStatus(snap *core.UsageSnapshot) {
	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}
}

func RequireAPIKey(acct core.AccountConfig, providerID string) (string, *core.UsageSnapshot) {
	apiKey := acct.ResolveAPIKey()
	if apiKey != "" {
		return apiKey, nil
	}
	snap := core.NewAuthSnapshot(providerID, acct.ID,
		fmt.Sprintf("no API key (set %s or configure token)", acct.APIKeyEnv))
	return "", &snap
}

func ResolveBaseURL(acct core.AccountConfig, defaultURL string) string {
	if acct.BaseURL != "" {
		return acct.BaseURL
	}
	return defaultURL
}

// FetchJSON performs an authenticated GET request and decodes the JSON response
// body into out. Returns the HTTP status code and response headers on success.
// For non-200 responses, returns an error with the status code.
// If client is nil a default client with a 30-second timeout is used.
func FetchJSON(ctx context.Context, url, apiKey string, out any, client *http.Client) (int, http.Header, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return resp.StatusCode, resp.Header, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, resp.Header, fmt.Errorf("reading body: %w", err)
	}

	if out != nil && len(body) > 0 {
		if err := json.Unmarshal(body, out); err != nil {
			return resp.StatusCode, resp.Header, fmt.Errorf("parsing response: %w", err)
		}
	}

	return resp.StatusCode, resp.Header, nil
}

// ProbeRateLimits performs a GET request to the given URL with Bearer auth,
// copies redacted headers to snap.Raw, applies standard status code handling
// (401/403 → StatusAuth, 429 → StatusLimited), and parses standard RPM/TPM
// rate-limit headers. If client is nil a default client with a 30-second
// timeout is used.
func ProbeRateLimits(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot, client *http.Client) error {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}

	ApplyStatusFromResponse(resp, snap)
	if snap.Status == core.StatusAuth {
		return nil
	}

	ApplyStandardRateLimits(resp, snap)
	return nil
}

// AnyPathModifiedAfter returns true if any of the given paths has an mtime
// after since. For a directory, files anywhere below it count too: appending
// to a nested session log does not move the directory's own mtime (see
// treeModifiedAfter for how this stays cheap). Paths that don't exist or
// can't be stat'd are silently skipped.
func AnyPathModifiedAfter(paths []string, since time.Time) bool {
	for _, path := range paths {
		if path == "" {
			continue
		}
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.ModTime().After(since) {
			return true
		}
		if info.IsDir() && treeModifiedAfter(path, since) {
			return true
		}
	}
	return false
}
