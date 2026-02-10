package openai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://api.openai.com/v1"
	defaultModel   = "gpt-4.1-mini"

	pricingSummary = "gpt-4.1: $2/$8 · gpt-4.1-mini: $0.40/$1.60 · gpt-4.1-nano: $0.10/$0.40 · " +
		"o3: $10/$40 · o4-mini: $1.10/$4.40 · gpt-4o: $2.50/$10 · gpt-4o-mini: $0.15/$0.60 " +
		"(input/output per 1M tokens)"
)

var modelPricingTable = map[string]modelPricing{
	"gpt-4.1":                {InputPerMillion: 2.00, OutputPerMillion: 8.00, CachedInputPerMillion: 0.50},
	"gpt-4.1-mini":           {InputPerMillion: 0.40, OutputPerMillion: 1.60, CachedInputPerMillion: 0.10},
	"gpt-4.1-nano":           {InputPerMillion: 0.10, OutputPerMillion: 0.40, CachedInputPerMillion: 0.025},
	"gpt-4o":                 {InputPerMillion: 2.50, OutputPerMillion: 10.00, CachedInputPerMillion: 1.25},
	"gpt-4o-mini":            {InputPerMillion: 0.15, OutputPerMillion: 0.60, CachedInputPerMillion: 0.075},
	"gpt-4o-audio":           {InputPerMillion: 2.50, OutputPerMillion: 10.00, CachedInputPerMillion: 1.25},
	"o3":                     {InputPerMillion: 10.00, OutputPerMillion: 40.00, CachedInputPerMillion: 2.50},
	"o3-mini":                {InputPerMillion: 1.10, OutputPerMillion: 4.40, CachedInputPerMillion: 0.275},
	"o4-mini":                {InputPerMillion: 1.10, OutputPerMillion: 4.40, CachedInputPerMillion: 0.275},
	"o1":                     {InputPerMillion: 15.00, OutputPerMillion: 60.00, CachedInputPerMillion: 7.50},
	"o1-mini":                {InputPerMillion: 1.10, OutputPerMillion: 4.40, CachedInputPerMillion: 0.55},
	"o1-pro":                 {InputPerMillion: 150.00, OutputPerMillion: 600.00, CachedInputPerMillion: 75.00},
	"gpt-4-turbo":            {InputPerMillion: 10.00, OutputPerMillion: 30.00, CachedInputPerMillion: 5.00},
	"gpt-3.5-turbo":          {InputPerMillion: 0.50, OutputPerMillion: 1.50, CachedInputPerMillion: 0.25},
	"text-embedding-3-small": {InputPerMillion: 0.02, OutputPerMillion: 0},
	"text-embedding-3-large": {InputPerMillion: 0.13, OutputPerMillion: 0},
}

type modelPricing struct {
	InputPerMillion       float64
	OutputPerMillion      float64
	CachedInputPerMillion float64
}

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "openai" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "OpenAI",
		Capabilities: []string{"headers"},
		DocURL:       "https://platform.openai.com/docs/guides/rate-limits",
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
			Message:    "no API key found (set OPENAI_API_KEY or configure token)",
		}, nil
	}

	baseURL := acct.BaseURL
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	model := acct.ProbeModel
	if model == "" {
		model = defaultModel
	}

	url := baseURL + "/models/" + model
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("openai: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return core.QuotaSnapshot{}, fmt.Errorf("openai: request failed: %w", err)
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
	}

	parsers.ApplyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	snap.Raw["pricing_summary"] = pricingSummary
	if pricing, ok := modelPricingTable[model]; ok {
		snap.Raw["probe_model_input_price"] = fmt.Sprintf("$%.2f/1M tokens", pricing.InputPerMillion)
		snap.Raw["probe_model_output_price"] = fmt.Sprintf("$%.2f/1M tokens", pricing.OutputPerMillion)
	}

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}
