package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://api.anthropic.com/v1"
)

// ── Model pricing (USD per 1M tokens, as of early 2026) ────────────────────
// Source: https://docs.anthropic.com/en/docs/about-claude/models
type modelPricing struct {
	InputPerMillion       float64
	OutputPerMillion      float64
	CacheReadPerMillion   float64
	CacheCreatePerMillion float64
}

var modelPricingTable = map[string]modelPricing{
	// Claude Opus 4 family
	"claude-opus-4-6":          {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	"claude-opus-4-5-20251101": {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	// Claude Sonnet 4 family
	"claude-sonnet-4-5-20250929": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	"claude-sonnet-4-20250514":   {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	// Claude Haiku 3.5
	"claude-haiku-3-5-20241022": {InputPerMillion: 0.80, OutputPerMillion: 4.0, CacheReadPerMillion: 0.08, CacheCreatePerMillion: 1.0},
	// Claude 3 Opus (legacy)
	"claude-3-opus-20240229": {InputPerMillion: 15.0, OutputPerMillion: 75.0, CacheReadPerMillion: 1.50, CacheCreatePerMillion: 18.75},
	// Claude 3 Sonnet (legacy)
	"claude-3-sonnet-20240229": {InputPerMillion: 3.0, OutputPerMillion: 15.0, CacheReadPerMillion: 0.30, CacheCreatePerMillion: 3.75},
	// Claude 3 Haiku (legacy)
	"claude-3-haiku-20240307": {InputPerMillion: 0.25, OutputPerMillion: 1.25, CacheReadPerMillion: 0.03, CacheCreatePerMillion: 0.30},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "opus-4: $15/$75 · sonnet-4: $3/$15 · haiku-3.5: $0.80/$4.00 " +
		"(input/output per 1M tokens; cache read ~10%, cache write ~125% of input)"
}

// Provider implements core.QuotaProvider for the Anthropic Claude API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "anthropic" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Anthropic",
		Capabilities: []string{"headers"},
		DocURL:       "https://docs.anthropic.com/en/api/rate-limits",
	}
}

// Fetch makes a lightweight API call to parse Anthropic rate-limit headers:
//
//	anthropic-ratelimit-requests-limit
//	anthropic-ratelimit-requests-remaining
//	anthropic-ratelimit-requests-reset
//	anthropic-ratelimit-tokens-limit
//	anthropic-ratelimit-tokens-remaining
//	anthropic-ratelimit-tokens-reset
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.QuotaSnapshot, error) {
	apiKey := acct.Token
	if apiKey == "" {
		apiKey = os.Getenv(acct.APIKeyEnv)
	}
	if apiKey == "" {
		return core.QuotaSnapshot{
			ProviderID: p.ID(),
			AccountID:  acct.ID,
			Timestamp:  time.Now(),
			Status:     core.StatusAuth,
			Message:    "no API key found (set ANTHROPIC_API_KEY or configure token)",
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	// Use the messages endpoint with a minimal request to trigger rate-limit headers.
	// We send a request that will fail fast but still return headers.
	url := baseURL + "/messages"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("anthropic: creating request: %w", err)
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("anthropic: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        parsers.RedactHeaders(resp.Header, "x-api-key"),
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return snap, nil
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
	}

	// Parse request limits.
	parseAnthropicGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"anthropic-ratelimit-requests-limit",
		"anthropic-ratelimit-requests-remaining",
		"anthropic-ratelimit-requests-reset",
	)

	// Parse token limits.
	parseAnthropicGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"anthropic-ratelimit-tokens-limit",
		"anthropic-ratelimit-tokens-remaining",
		"anthropic-ratelimit-tokens-reset",
	)

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingSummary()

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}

func parseAnthropicGroup(h http.Header, snap *core.QuotaSnapshot, key, unit, window, limitH, remainH, resetH string) {
	limit := parsers.ParseFloat(h.Get(limitH))
	remaining := parsers.ParseFloat(h.Get(remainH))

	if limit != nil || remaining != nil {
		snap.Metrics[key] = core.Metric{
			Limit:     limit,
			Remaining: remaining,
			Unit:      unit,
			Window:    window,
		}
	}

	if rt := parsers.ParseResetTime(h.Get(resetH)); rt != nil {
		snap.Resets[key+"_reset"] = *rt
	}
}
