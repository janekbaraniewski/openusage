package groq

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://api.groq.com/openai/v1"

	pricingSummary = "llama-3.3-70b: $0.59/$0.79 · llama-3.1-8b: $0.05/$0.08 · " +
		"mixtral-8x7b: $0.24/$0.24 · deepseek-r1-distill-70b: $0.75/$0.99 · " +
		"qwen-qwq-32b: $0.29/$0.39 (input/output per 1M tokens)"
)

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
	apiKey := acct.ResolveAPIKey()
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

	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpd", "requests", "1d",
		"x-ratelimit-limit-requests-day", "x-ratelimit-remaining-requests-day", "x-ratelimit-reset-requests-day")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpd", "tokens", "1d",
		"x-ratelimit-limit-tokens-day", "x-ratelimit-remaining-tokens-day", "x-ratelimit-reset-tokens-day")

	snap.Raw["pricing_summary"] = pricingSummary

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = buildStatusMessage(snap)
	}

	return snap, nil
}

func buildStatusMessage(snap core.QuotaSnapshot) string {
	var parts []string
	for _, key := range []string{"rpm", "rpd"} {
		if m, ok := snap.Metrics[key]; ok && m.Remaining != nil && m.Limit != nil {
			label := "RPM"
			if key == "rpd" {
				label = "RPD"
			}
			parts = append(parts, fmt.Sprintf("%.0f/%.0f %s", *m.Remaining, *m.Limit, label))
		}
	}
	if len(parts) == 0 {
		return "OK"
	}
	msg := "Remaining: " + parts[0]
	for _, p := range parts[1:] {
		msg += ", " + p
	}
	return msg
}
