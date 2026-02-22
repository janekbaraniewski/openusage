package providers

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
)

// Helper function to create a standard HTTP request for providers
func CreateStandardRequest(ctx context.Context, baseURL, endpoint, apiKey string, headers map[string]string) (*http.Request, error) {
	url := baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	// Set headers
	for key, value := range headers {
		req.Header.Set(key, value)
	}

	// Add API key to authorization header if not already set
	if _, hasAuth := headers["Authorization"]; !hasAuth {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	return req, nil
}

// Helper function to handle standard response processing for providers
func ProcessStandardResponse(resp *http.Response, acct core.AccountConfig, providerID string, redactHeaders ...string) (core.UsageSnapshot, error) {
	snap := core.UsageSnapshot{
		ProviderID: providerID,
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        parsers.RedactHeaders(resp.Header, redactHeaders...),
	}

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

// Helper function to apply standard rate limit parsing for providers
func ApplyStandardRateLimits(resp *http.Response, snap *core.UsageSnapshot) {
	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")
}
