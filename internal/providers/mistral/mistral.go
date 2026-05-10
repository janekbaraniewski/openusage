package mistral

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const defaultBaseURL = "https://api.mistral.ai/v1"

type subscriptionResponse struct {
	ID            string   `json:"id"`
	Plan          string   `json:"plan"`
	MonthlyBudget *float64 `json:"monthly_budget"`
	CreditBalance *float64 `json:"credit_balance"`
}

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

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "mistral",
			Info: core.ProviderInfo{
				Name:         "Mistral AI",
				Capabilities: []string{"headers", "billing_subscription", "billing_usage"},
				DocURL:       "https://docs.mistral.ai/getting-started/models/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "MISTRAL_API_KEY",
				DefaultAccountID: "mistral",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set MISTRAL_API_KEY to a valid Mistral API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleFlamingo)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	if err := p.fetchSubscription(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["subscription_error"] = err.Error()
	}

	if err := p.fetchUsage(ctx, baseURL, apiKey, &snap); err != nil {
		snap.Raw["usage_error"] = err.Error()
	}

	if err := p.fetchRateLimits(ctx, baseURL, apiKey, &snap); err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, err
	}

	shared.FinalizeStatus(&snap)
	return snap, nil
}

func (p *Provider) fetchSubscription(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	var sub subscriptionResponse
	if _, _, err := shared.FetchJSON(ctx, baseURL+"/billing/subscription", apiKey, &sub, p.Client()); err != nil {
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

func (p *Provider) fetchUsage(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	url := fmt.Sprintf("%s/billing/usage?start_date=%s&end_date=%s",
		baseURL,
		start.Format("2006-01-02"),
		now.Format("2006-01-02"),
	)

	var usage usageResponse
	if _, _, err := shared.FetchJSON(ctx, url, apiKey, &usage, p.Client()); err != nil {
		return err
	}

	totalCost := usage.TotalCost
	spendMetric := core.Metric{
		Used:   &totalCost,
		Unit:   "EUR",
		Window: "1mo",
	}

	if m, ok := snap.Metrics["monthly_budget"]; ok && m.Limit != nil {
		remaining := *m.Limit - totalCost
		spendMetric.Limit = m.Limit
		spendMetric.Remaining = &remaining
	}
	snap.Metrics["monthly_spend"] = spendMetric

	var totalInput, totalOutput int64
	for _, d := range usage.Data {
		totalInput += d.InputTokens
		totalOutput += d.OutputTokens
	}

	if totalInput > 0 || totalOutput > 0 {
		inp := float64(totalInput)
		out := float64(totalOutput)
		snap.Metrics["monthly_input_tokens"] = core.Metric{Used: &inp, Unit: "tokens", Window: "1mo"}
		snap.Metrics["monthly_output_tokens"] = core.Metric{Used: &out, Unit: "tokens", Window: "1mo"}
	}

	snap.Raw["monthly_cost"] = fmt.Sprintf("%.4f EUR", totalCost)

	return nil
}

func (p *Provider) fetchRateLimits(ctx context.Context, baseURL, apiKey string, snap *core.UsageSnapshot) error {
	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("mistral: creating request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := p.Client().Do(req)
	if err != nil {
		return fmt.Errorf("mistral: request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}

	// Centralised 401/403/429 mapping. Mistral has no provider-specific
	// status codes to override, so the shared switch is sufficient.
	shared.ApplyStatusFromResponse(resp, snap)
	if snap.Status == core.StatusAuth {
		return nil
	}

	// Mistral exposes three rate-limit header groups: unprefixed
	// `ratelimit-*` (canonical RPM), per-request `x-ratelimit-*-requests`,
	// and per-token `x-ratelimit-*-tokens`. The shared ApplyStandardRateLimits
	// only knows the second-and-third pattern, so we apply each explicitly.
	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm", "requests", "1m",
		"ratelimit-limit", "ratelimit-remaining", "ratelimit-reset")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm_alt", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	return nil
}
