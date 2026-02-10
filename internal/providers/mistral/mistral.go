// Package mistral implements a QuotaProvider for the Mistral AI API.
//
// Mistral provides standard rate-limit headers (ratelimit-limit/remaining/reset)
// and also supports OpenAI-compatible x-ratelimit-* headers on some endpoints.
//
// Additionally, Mistral has a /v1/billing/subscription endpoint that returns
// the active subscription plan, and /v1/billing/usage for monthly usage data
// including token counts per model.
//
// Rate limit headers from GET /v1/models:
//   - ratelimit-limit / ratelimit-remaining / ratelimit-reset
//   - x-ratelimit-limit-requests / x-ratelimit-remaining-requests
//   - x-ratelimit-limit-tokens / x-ratelimit-remaining-tokens
package mistral

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
	defaultBaseURL = "https://api.mistral.ai/v1"
)

// ── Model pricing (EUR per 1M tokens, as of early 2026) ────────────────────
// Source: https://mistral.ai/products/la-plateforme#pricing
type modelPricing struct {
	InputPerMillion  float64 // EUR
	OutputPerMillion float64 // EUR
}

var modelPricingTable = map[string]modelPricing{
	// Premier models
	"mistral-large-latest": {InputPerMillion: 2.00, OutputPerMillion: 6.00},
	"mistral-large-2411":   {InputPerMillion: 2.00, OutputPerMillion: 6.00},
	"pixtral-large-latest": {InputPerMillion: 2.00, OutputPerMillion: 6.00},
	"pixtral-large-2411":   {InputPerMillion: 2.00, OutputPerMillion: 6.00},
	// Mistral Medium
	"mistral-medium-latest": {InputPerMillion: 0.40, OutputPerMillion: 2.00},
	"mistral-medium-2505":   {InputPerMillion: 0.40, OutputPerMillion: 2.00},
	// Mistral Small
	"mistral-small-latest": {InputPerMillion: 0.10, OutputPerMillion: 0.30},
	"mistral-small-2503":   {InputPerMillion: 0.10, OutputPerMillion: 0.30},
	// Code models
	"codestral-latest": {InputPerMillion: 0.30, OutputPerMillion: 0.90},
	"codestral-2501":   {InputPerMillion: 0.30, OutputPerMillion: 0.90},
	// Ministral (edge)
	"ministral-8b-latest": {InputPerMillion: 0.10, OutputPerMillion: 0.10},
	"ministral-3b-latest": {InputPerMillion: 0.04, OutputPerMillion: 0.04},
	// Pixtral (vision)
	"pixtral-12b-2409": {InputPerMillion: 0.15, OutputPerMillion: 0.15},
	// Mistral Nemo
	"open-mistral-nemo": {InputPerMillion: 0.15, OutputPerMillion: 0.15},
	// Embeddings
	"mistral-embed": {InputPerMillion: 0.10, OutputPerMillion: 0},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "large: €2/€6 · medium: €0.40/€2 · small: €0.10/€0.30 · " +
		"codestral: €0.30/€0.90 · ministral-8b: €0.10/€0.10 · ministral-3b: €0.04/€0.04 " +
		"(input/output per 1M tokens, EUR)"
}

// subscriptionResponse from GET /billing/subscription
type subscriptionResponse struct {
	ID            string   `json:"id"`
	Plan          string   `json:"plan"` // "free", "starter", "enterprise"
	MonthlyBudget *float64 `json:"monthly_budget"`
	CreditBalance *float64 `json:"credit_balance"`
}

// usageResponse from GET /billing/usage with query params
type usageResponse struct {
	Object    string      `json:"object"`
	Data      []usageData `json:"data"`
	TotalCost float64     `json:"total_cost"`
}

type usageData struct {
	Model        string  `json:"model"`
	InputTokens  int64   `json:"input_tokens"`
	OutputTokens int64   `json:"output_tokens"`
	TotalCost    float64 `json:"total_cost"`
}

// Provider implements core.QuotaProvider for the Mistral AI API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "mistral" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "Mistral AI",
		Capabilities: []string{"headers", "billing_subscription", "billing_usage"},
		DocURL:       "https://docs.mistral.ai/getting-started/models/",
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

	// 1. Try billing/subscription for plan info
	if err := p.fetchSubscription(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["subscription_error"] = err.Error()
	}

	// 2. Try billing/usage for monthly usage
	if err := p.fetchUsage(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["usage_error"] = err.Error()
	}

	// 3. Hit /models for rate-limit headers
	modelsURL := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return snap, fmt.Errorf("mistral: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, fmt.Errorf("mistral: request failed: %w", err)
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

	// Standard Mistral headers
	applyRateLimitGroup(resp.Header, &snap, "rpm", "requests", "1m",
		"ratelimit-limit", "ratelimit-remaining", "ratelimit-reset")
	applyRateLimitGroup(resp.Header, &snap, "rpm_alt", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	applyRateLimitGroup(resp.Header, &snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	// Add pricing reference data
	snap.Raw["pricing_summary"] = pricingSummary()

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}

func (p *Provider) fetchSubscription(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	url := baseURL + "/billing/subscription"
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

	var sub subscriptionResponse
	if err := json.Unmarshal(body, &sub); err != nil {
		return err
	}

	if sub.Plan != "" {
		snap.Raw["plan"] = sub.Plan
	}

	if sub.MonthlyBudget != nil {
		snap.Metrics["monthly_budget"] = core.Metric{
			Limit:  sub.MonthlyBudget,
			Unit:   "EUR",
			Window: "1mo",
		}
	}

	if sub.CreditBalance != nil {
		snap.Metrics["credit_balance"] = core.Metric{
			Remaining: sub.CreditBalance,
			Unit:      "EUR",
			Window:    "current",
		}
	}

	return nil
}

func (p *Provider) fetchUsage(ctx context.Context, baseURL, apiKey string, snap *core.QuotaSnapshot) error {
	// Get this month's usage
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	url := fmt.Sprintf("%s/billing/usage?start_date=%s&end_date=%s",
		baseURL,
		start.Format("2006-01-02"),
		now.Format("2006-01-02"),
	)

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

	var usage usageResponse
	if err := json.Unmarshal(body, &usage); err != nil {
		return err
	}

	totalCost := usage.TotalCost
	snap.Metrics["monthly_spend"] = core.Metric{
		Used:   &totalCost,
		Unit:   "EUR",
		Window: "1mo",
	}

	// If we have a budget, calculate remaining
	if m, ok := snap.Metrics["monthly_budget"]; ok && m.Limit != nil {
		remaining := *m.Limit - totalCost
		snap.Metrics["monthly_spend"] = core.Metric{
			Limit:     m.Limit,
			Used:      &totalCost,
			Remaining: &remaining,
			Unit:      "EUR",
			Window:    "1mo",
		}
	}

	// Aggregate token counts
	var totalInput, totalOutput int64
	for _, d := range usage.Data {
		totalInput += d.InputTokens
		totalOutput += d.OutputTokens
	}

	if totalInput > 0 || totalOutput > 0 {
		inp := float64(totalInput)
		out := float64(totalOutput)
		snap.Metrics["monthly_input_tokens"] = core.Metric{
			Used:   &inp,
			Unit:   "tokens",
			Window: "1mo",
		}
		snap.Metrics["monthly_output_tokens"] = core.Metric{
			Used:   &out,
			Unit:   "tokens",
			Window: "1mo",
		}
	}

	snap.Raw["monthly_cost"] = fmt.Sprintf("%.4f EUR", totalCost)

	return nil
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
