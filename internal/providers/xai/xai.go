// Package xai implements a QuotaProvider for the xAI (Grok) API.
//
// xAI uses an OpenAI-compatible API with standard rate-limit headers.
// It also provides a /v1/api-key endpoint that returns information about
// the API key including its name, acls, team/user info, and remaining credits.
//
// Rate-limit headers from all endpoints:
//   - x-ratelimit-limit-requests / x-ratelimit-remaining-requests
//   - x-ratelimit-limit-tokens / x-ratelimit-remaining-tokens
//   - x-ratelimit-reset-requests / x-ratelimit-reset-tokens
package xai

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://api.x.ai/v1"
)

// ── Model pricing (USD per 1M tokens, as of early 2026) ────────────────────
// Source: https://docs.x.ai/docs
type modelPricing struct {
	InputPerMillion  float64
	OutputPerMillion float64
}

var modelPricingTable = map[string]modelPricing{
	// Grok 3 family
	"grok-3":           {InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"grok-3-fast":      {InputPerMillion: 5.00, OutputPerMillion: 25.00},
	"grok-3-mini":      {InputPerMillion: 0.30, OutputPerMillion: 0.50},
	"grok-3-mini-fast": {InputPerMillion: 0.60, OutputPerMillion: 4.00},
	// Grok 2 family
	"grok-2":        {InputPerMillion: 2.00, OutputPerMillion: 10.00},
	"grok-2-latest": {InputPerMillion: 2.00, OutputPerMillion: 10.00},
	"grok-2-mini":   {InputPerMillion: 0.20, OutputPerMillion: 1.00},
	// Grok Vision
	"grok-2-vision":      {InputPerMillion: 2.00, OutputPerMillion: 10.00},
	"grok-2-vision-1212": {InputPerMillion: 2.00, OutputPerMillion: 10.00},
	// Grok (legacy)
	"grok-beta": {InputPerMillion: 5.00, OutputPerMillion: 15.00},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "grok-3: $3/$15 · grok-3-mini: $0.30/$0.50 · " +
		"grok-2: $2/$10 · grok-2-mini: $0.20/$1 " +
		"(input/output per 1M tokens)"
}

// apiKeyResponse is the JSON returned by GET /api-key.
type apiKeyResponse struct {
	Name       string `json:"name"`
	APIKeyID   string `json:"api_key_id"`
	TeamID     string `json:"team_id"`
	CreateTime string `json:"create_time"`
	ModifyTime string `json:"modify_time"`
	ACLS       struct {
		AllowedModels []string `json:"allowed_models"`
	} `json:"acls"`
	// Credit info returned on some tiers
	RemainingBalance *float64 `json:"remaining_balance"`
	SpentBalance     *float64 `json:"spent_balance"`
	TotalGranted     *float64 `json:"total_granted"`
}

// Provider implements core.QuotaProvider for xAI (Grok) API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "xai" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "xAI (Grok)",
		Capabilities: []string{"headers", "api_key_info"},
		DocURL:       "https://docs.x.ai/docs",
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

	snap := core.QuotaSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	// 1. Try /api-key for key info and balance
	if err := p.fetchAPIKeyInfo(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["api_key_info_error"] = err.Error()
	}

	// 2. Hit /models for rate-limit headers
	modelsURL := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return snap, fmt.Errorf("xai: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, fmt.Errorf("xai: request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
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

	// OpenAI-compatible headers
	parseGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parseGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingSummary()

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}

func (p *Provider) fetchAPIKeyInfo(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/api-key"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var keyInfo apiKeyResponse
	if err := json.Unmarshal(body, &keyInfo); err != nil {
		return err
	}

	if keyInfo.Name != "" {
		snap.Raw["api_key_name"] = keyInfo.Name
	}
	if keyInfo.TeamID != "" {
		snap.Raw["team_id"] = keyInfo.TeamID
	}

	// Balance info
	if keyInfo.RemainingBalance != nil {
		snap.Metrics["credits"] = core.Metric{
			Remaining: keyInfo.RemainingBalance,
			Unit:      "USD",
			Window:    "current",
		}
		if keyInfo.SpentBalance != nil {
			m := snap.Metrics["credits"]
			m.Used = keyInfo.SpentBalance
			snap.Metrics["credits"] = m
		}
		if keyInfo.TotalGranted != nil {
			m := snap.Metrics["credits"]
			m.Limit = keyInfo.TotalGranted
			snap.Metrics["credits"] = m
		}

		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("$%.2f remaining", *keyInfo.RemainingBalance)
	}

	return nil
}

func parseGroup(h http.Header, snap *core.QuotaSnapshot, key, unit, window, limitH, remainH, resetH string) {
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
