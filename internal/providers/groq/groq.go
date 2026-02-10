// Package groq implements a QuotaProvider for the Groq API.
//
// Groq uses OpenAI-compatible rate-limit headers on all endpoints:
//   - x-ratelimit-limit-requests / x-ratelimit-remaining-requests
//   - x-ratelimit-limit-tokens / x-ratelimit-remaining-tokens
//   - x-ratelimit-reset-requests / x-ratelimit-reset-tokens
//
// Groq also exposes daily request limits on some tiers via:
//   - x-ratelimit-limit-requests-day / x-ratelimit-remaining-requests-day
//
// There is no public /balance or /usage endpoint; rate-limit headers
// on the /models endpoint are the best signal.
package groq

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
	defaultBaseURL = "https://api.groq.com/openai/v1"
)

// ── Model pricing (USD per 1M tokens, as of early 2026) ────────────────────
// Source: https://groq.com/pricing/
// Groq offers extremely fast inference on open-source models.
// Free tier has rate limits; paid tier (GroqCloud) has higher limits.
type modelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

var modelPricingTable = map[string]modelPricing{
	// Llama 3.3 70B
	"llama-3.3-70b-versatile": {InputPerMillion: 0.59, OutputPerMillion: 0.79},
	"llama-3.3-70b-specdec":   {InputPerMillion: 0.59, OutputPerMillion: 0.99},
	// Llama 3.1 family
	"llama-3.1-8b-instant":    {InputPerMillion: 0.05, OutputPerMillion: 0.08},
	"llama-3.1-70b-versatile": {InputPerMillion: 0.59, OutputPerMillion: 0.79},
	"llama-3.1-405b":          {InputPerMillion: 0.59, OutputPerMillion: 0.79},
	// Llama 3 Guard
	"llama-guard-3-8b": {InputPerMillion: 0.20, OutputPerMillion: 0.20},
	// Mixtral
	"mixtral-8x7b-32768": {InputPerMillion: 0.24, OutputPerMillion: 0.24},
	// Gemma 2
	"gemma2-9b-it": {InputPerMillion: 0.20, OutputPerMillion: 0.20},
	// DeepSeek R1 Distill
	"deepseek-r1-distill-llama-70b": {InputPerMillion: 0.75, OutputPerMillion: 0.99},
	// Qwen
	"qwen-qwq-32b": {InputPerMillion: 0.29, OutputPerMillion: 0.39},
	// Compound AI (tool use)
	"compound-beta":      {InputPerMillion: 0.59, OutputPerMillion: 0.79},
	"compound-beta-mini": {InputPerMillion: 0.05, OutputPerMillion: 0.08},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "llama-3.3-70b: $0.59/$0.79 · llama-3.1-8b: $0.05/$0.08 · " +
		"mixtral-8x7b: $0.24/$0.24 · deepseek-r1-distill-70b: $0.75/$0.99 · " +
		"qwen-qwq-32b: $0.29/$0.39 (input/output per 1M tokens)"
}

// Provider implements core.QuotaProvider for Groq API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "groq" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Groq",
		Capabilities: []string{"headers", "daily_limits"},
		DocURL:       "https://console.groq.com/docs/rate-limits",
	}
}

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
			Message:    fmt.Sprintf("env var %s not set", acct.APIKeyEnv),
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}

	// Use /models endpoint — lightweight GET that returns rate-limit headers
	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("groq: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("groq: request failed: %w", err)
	}
	defer resp.Body.Close()

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        parsers.RedactHeaders(resp.Header),
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return snap, nil
	}

	if resp.StatusCode == http.StatusTooManyRequests {
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"

		// Still parse headers on 429 — they contain retry info
		retryAfter := resp.Header.Get("Retry-After")
		if retryAfter != "" {
			snap.Raw["retry_after"] = retryAfter
		}
	}

	// Per-minute rate limits
	applyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	applyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	// Per-day rate limits (Groq-specific, available on some tiers)
	applyRateLimitGroup(resp.Header, &snap, "rpd", "requests", "1d",
		"x-ratelimit-limit-requests-day", "x-ratelimit-remaining-requests-day", "x-ratelimit-reset-requests-day")
	applyRateLimitGroup(resp.Header, &snap, "tpd", "tokens", "1d",
		"x-ratelimit-limit-tokens-day", "x-ratelimit-remaining-tokens-day", "x-ratelimit-reset-tokens-day")

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingSummary()

	if snap.Status == "" {
		snap.Status = core.StatusOK
		msgs := []string{}
		if m, ok := snap.Metrics["rpm"]; ok && m.Remaining != nil && m.Limit != nil {
			msgs = append(msgs, fmt.Sprintf("%.0f/%.0f RPM", *m.Remaining, *m.Limit))
		}
		if m, ok := snap.Metrics["rpd"]; ok && m.Remaining != nil && m.Limit != nil {
			msgs = append(msgs, fmt.Sprintf("%.0f/%.0f RPD", *m.Remaining, *m.Limit))
		}
		if len(msgs) > 0 {
			snap.Message = "Remaining: " + msgs[0]
			for _, m := range msgs[1:] {
				snap.Message += ", " + m
			}
		} else {
			snap.Message = "OK"
		}
	}

	return snap, nil
}

func applyRateLimitGroup(h http.Header, snap *core.QuotaSnapshot, key, unit, window, limitH, remainH, resetH string) {
	rlg := parsers.ParseRateLimitGroup(h, limitH, remainH, resetH)
	if rlg == nil {
		return
	}
	snap.Metrics[key] = core.Metric{
		Limit:     rlg.Limit,
		Remaining: rlg.Remaining,
		Unit:      unit,
		Window:    window,
	}
	if rlg.ResetTime != nil {
		snap.Resets[key+"_reset"] = *rlg.ResetTime
	}
}
