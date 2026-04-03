package alibaba_cloud

import (
	"context"
	"fmt"
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
	var quotasResp quotasResponse
	statusCode, _, err := shared.FetchJSON(ctx, baseURL+"/quotas", apiKey, &quotasResp, p.Client())
	if err != nil {
		// FetchJSON returns an error for non-200 status codes; handle gracefully.
		switch {
		case statusCode == http.StatusUnauthorized || statusCode == http.StatusForbidden:
			snap.Status = core.StatusAuth
			snap.Message = "Invalid or expired API key"
			return snap, nil
		case statusCode == http.StatusTooManyRequests:
			snap.Status = core.StatusLimited
			snap.Message = "Rate limited (HTTP 429)"
			return snap, nil
		case statusCode > 0 && statusCode != http.StatusOK:
			snap.Status = core.StatusError
			snap.Message = fmt.Sprintf("HTTP %d error", statusCode)
			return snap, nil
		default:
			// Network errors (statusCode==0) or parse errors (statusCode==200)
			return core.UsageSnapshot{}, fmt.Errorf("alibaba_cloud: fetching quotas: %w", err)
		}
	}

	// Check for API-level errors in response body
	if quotasResp.Code != "" && quotasResp.Code != "Success" {
		return core.UsageSnapshot{}, fmt.Errorf("alibaba_cloud: API error: %s - %s", quotasResp.Code, quotasResp.Message)
	}

	quotasData := &quotasResp.Data

	// Parse rate limits
	if quotasData.RateLimit != nil {
		if quotasData.RateLimit.RPM != nil {
			snap.Metrics["rpm"] = core.Metric{
				Limit:  func(v int) *float64 { f := float64(v); return &f }(*quotasData.RateLimit.RPM),
				Unit:   "requests",
				Window: "1m",
			}
		}
		if quotasData.RateLimit.TPM != nil {
			snap.Metrics["tpm"] = core.Metric{
				Limit:  func(v int) *float64 { f := float64(v); return &f }(*quotasData.RateLimit.TPM),
				Unit:   "tokens",
				Window: "1m",
			}
		}
	}

	// Parse credits and balance
	if quotasData.Credits != nil {
		snap.Metrics["credit_balance"] = core.Metric{
			Limit:  quotasData.Credits,
			Unit:   "USD",
			Window: "current",
		}
	}

	if quotasData.Available != nil {
		snap.Metrics["available_balance"] = core.Metric{
			Limit:  quotasData.Available,
			Unit:   "USD",
			Window: "current",
		}
	}

	if quotasData.SpendLimit != nil {
		snap.Metrics["spend_limit"] = core.Metric{
			Limit:  quotasData.SpendLimit,
			Unit:   "USD",
			Window: "current",
		}
	}

	// Parse spending
	if quotasData.DailySpend != nil {
		snap.Metrics["daily_spend"] = core.Metric{
			Used:   quotasData.DailySpend,
			Unit:   "USD",
			Window: "1d",
		}
	}

	if quotasData.MonthlySpend != nil {
		snap.Metrics["monthly_spend"] = core.Metric{
			Used:   quotasData.MonthlySpend,
			Unit:   "USD",
			Window: "30d",
		}
	}

	// Parse usage counts
	if quotasData.TokensUsed != nil {
		snap.Metrics["tokens_used"] = core.Metric{
			Used:   quotasData.TokensUsed,
			Unit:   "tokens",
			Window: "current",
		}
	}

	if quotasData.RequestsUsed != nil {
		snap.Metrics["requests_used"] = core.Metric{
			Used:   quotasData.RequestsUsed,
			Unit:   "requests",
			Window: "current",
		}
	}

	// Parse per-model quotas
	if quotasData.Models != nil {
		for modelName, modelQuota := range quotasData.Models {
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
	if quotasData.BillingPeriod != nil {
		snap.SetAttribute("billing_cycle_start", quotasData.BillingPeriod.Start)
		snap.SetAttribute("billing_cycle_end", quotasData.BillingPeriod.End)
	}

	snap.Status = core.StatusOK
	snap.Message = "OK"

	return snap, nil
}
