package shared

import (
	"context"
	"fmt"
	"net/http"

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

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return snap, nil
	case http.StatusTooManyRequests:
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
		if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
			snap.Raw["retry_after"] = retryAfter
		}
	}
	return snap, nil
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
