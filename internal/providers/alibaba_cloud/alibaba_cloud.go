package alibaba_cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL = "https://dashscope.aliyuncs.com/api/v1"
)

type quotasResponse struct {
	Code      string     `json:"code"`
	Message   string     `json:"message"`
	Data      quotasData `json:"data"`
	RequestID string     `json:"request_id"`
}

type quotasData struct {
	Available     *float64              `json:"available"`
	Credits       *float64              `json:"credits"`
	SpendLimit    *float64              `json:"spend_limit"`
	DailySpend    *float64              `json:"daily_spend"`
	MonthlySpend  *float64              `json:"monthly_spend"`
	Usage         *float64              `json:"usage"`
	TokensUsed    *float64              `json:"tokens_used"`
	RequestsUsed  *float64              `json:"requests_used"`
	RateLimit     *rateLimitInfo        `json:"rate_limit"`
	Models        map[string]modelQuota `json:"models"`
	BillingPeriod *billingPeriod        `json:"billing_period"`
}

type rateLimitInfo struct {
	RPM       *int   `json:"rpm"`
	TPM       *int   `json:"tpm"`
	Remaining *int   `json:"remaining"`
	ResetTime *int64 `json:"reset_time"`
}

type modelQuota struct {
	RPM   *int     `json:"rpm"`
	TPM   *int     `json:"tpm"`
	Used  *float64 `json:"used"`
	Limit *float64 `json:"limit"`
}

type billingPeriod struct {
	Start string `json:"start"`
	End   string `json:"end"`
}

type errorResponse struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id"`
}

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "alibaba_cloud",
			Info: core.ProviderInfo{
				Name:         "Alibaba Cloud Model Studios",
				Capabilities: []string{"quotas_endpoint", "credits", "rate_limits", "daily_usage", "per_model_tracking"},
				DocURL:       "https://dashscope.aliyun.com/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "ALIBABA_CLOUD_API_KEY",
				DefaultAccountID: "alibaba_cloud",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set ALIBABA_CLOUD_API_KEY to your DashScope API key.",
					"Get your key from: https://dashscope.aliyun.com/",
				},
			},
			Dashboard: dashboardWidget(),
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

	// Fetch quotas data
	quotasResp, statusCode, err := fetchQuotas(ctx, baseURL, apiKey)
	if err != nil {
		return core.UsageSnapshot{}, fmt.Errorf("alibaba_cloud: fetching quotas: %w", err)
	}

	// Handle HTTP error codes
	if statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden {
		snap.Status = core.StatusAuth
		snap.Message = "Invalid or expired API key"
		return snap, nil
	}

	if statusCode == http.StatusTooManyRequests {
		snap.Status = core.StatusLimited
		snap.Message = "Rate limited (HTTP 429)"
		return snap, nil
	}

	if statusCode != http.StatusOK {
		snap.Status = core.StatusError
		snap.Message = fmt.Sprintf("HTTP %d error", statusCode)
		return snap, nil
	}

	// Process the quotas response
	if quotasResp == nil {
		snap.Status = core.StatusOK
		snap.Message = "No quota data available"
		return snap, nil
	}

	// Parse rate limits
	if quotasResp.RateLimit != nil {
		if quotasResp.RateLimit.RPM != nil {
			snap.Metrics["rpm"] = core.Metric{
				Limit:  func(v int) *float64 { f := float64(v); return &f }(*quotasResp.RateLimit.RPM),
				Unit:   "requests",
				Window: "1m",
			}
		}
		if quotasResp.RateLimit.TPM != nil {
			snap.Metrics["tpm"] = core.Metric{
				Limit:  func(v int) *float64 { f := float64(v); return &f }(*quotasResp.RateLimit.TPM),
				Unit:   "tokens",
				Window: "1m",
			}
		}
	}

	// Parse credits and balance
	if quotasResp.Credits != nil {
		snap.Metrics["credit_balance"] = core.Metric{
			Limit:  quotasResp.Credits,
			Unit:   "USD",
			Window: "current",
		}
	}

	if quotasResp.Available != nil {
		snap.Metrics["available_balance"] = core.Metric{
			Limit:  quotasResp.Available,
			Unit:   "USD",
			Window: "current",
		}
	}

	if quotasResp.SpendLimit != nil {
		snap.Metrics["spend_limit"] = core.Metric{
			Limit:  quotasResp.SpendLimit,
			Unit:   "USD",
			Window: "current",
		}
	}

	// Parse spending
	if quotasResp.DailySpend != nil {
		snap.Metrics["daily_spend"] = core.Metric{
			Used:   quotasResp.DailySpend,
			Unit:   "USD",
			Window: "1d",
		}
	}

	if quotasResp.MonthlySpend != nil {
		snap.Metrics["monthly_spend"] = core.Metric{
			Used:   quotasResp.MonthlySpend,
			Unit:   "USD",
			Window: "30d",
		}
	}

	// Parse usage counts
	if quotasResp.TokensUsed != nil {
		snap.Metrics["tokens_used"] = core.Metric{
			Used:   quotasResp.TokensUsed,
			Unit:   "tokens",
			Window: "current",
		}
	}

	if quotasResp.RequestsUsed != nil {
		snap.Metrics["requests_used"] = core.Metric{
			Used:   quotasResp.RequestsUsed,
			Unit:   "requests",
			Window: "current",
		}
	}

	// Parse per-model quotas
	if quotasResp.Models != nil {
		for modelName, modelQuota := range quotasResp.Models {
			if modelQuota.Limit != nil && modelQuota.Used != nil {
				pctVal := (*modelQuota.Used / *modelQuota.Limit) * 100
				snap.Metrics[fmt.Sprintf("model_%s_usage_pct", modelName)] = core.Metric{
					Used:   &pctVal,
					Unit:   "%",
					Window: "current",
				}
				snap.Metrics[fmt.Sprintf("model_%s_used", modelName)] = core.Metric{
					Used:   modelQuota.Used,
					Limit:  modelQuota.Limit,
					Unit:   "units",
					Window: "current",
				}
			}
		}
	}

	// Set billing cycle dates as attributes
	if quotasResp.BillingPeriod != nil {
		snap.SetAttribute("billing_cycle_start", quotasResp.BillingPeriod.Start)
		snap.SetAttribute("billing_cycle_end", quotasResp.BillingPeriod.End)
	}

	snap.Status = core.StatusOK
	snap.Message = "OK"

	return snap, nil
}

func fetchQuotas(ctx context.Context, baseURL, apiKey string) (*quotasData, int, error) {
	url := baseURL + "/quotas"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, fmt.Errorf("creating request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("User-Agent", "openusage/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("reading response: %w", err)
	}

	// Handle rate limiting early
	if resp.StatusCode == http.StatusTooManyRequests {
		return nil, resp.StatusCode, nil
	}

	// Handle auth errors
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, resp.StatusCode, nil
	}

	// Parse response
	var quotasResp quotasResponse
	if err := json.Unmarshal(bodyBytes, &quotasResp); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("parsing response: %w", err)
	}

	// Check for API errors in response
	if quotasResp.Code != "" && quotasResp.Code != "Success" {
		return nil, resp.StatusCode, fmt.Errorf("API error: %s - %s", quotasResp.Code, quotasResp.Message)
	}

	return &quotasResp.Data, resp.StatusCode, nil
}
