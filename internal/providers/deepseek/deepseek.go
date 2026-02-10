// Package deepseek implements a QuotaProvider for the DeepSeek API.
//
// DeepSeek uses an OpenAI-compatible API but also provides a dedicated
// /user/balance endpoint that returns credit balance information:
//
//	GET https://api.deepseek.com/user/balance
//	Response: {"balance_infos": [{"currency": "CNY", "total_balance": "...",
//	           "granted_balance": "...", "topped_up_balance": "..."}], "is_available": true}
//
// We query both the balance endpoint and /models for rate-limit headers.
package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/parsers"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
)

// ── Model pricing (CNY per 1M tokens, as of early 2026) ────────────────────
// Source: https://platform.deepseek.com/api-docs/pricing
// DeepSeek prices in CNY. Approximate USD at ~7.2 CNY/USD.
type modelPricing struct {
	InputPerMillion       float64 // CNY
	OutputPerMillion      float64 // CNY
	CacheHitPerMillion    float64 // CNY (cached/prefix input)
	InputPerMillionUSD    float64 // approximate USD
	OutputPerMillionUSD   float64 // approximate USD
	CacheHitPerMillionUSD float64 // approximate USD
}

var modelPricingTable = map[string]modelPricing{
	// DeepSeek-V3 (also used as deepseek-chat)
	"deepseek-chat": {
		InputPerMillion: 0.50, OutputPerMillion: 2.19, CacheHitPerMillion: 0.10,
		InputPerMillionUSD: 0.07, OutputPerMillionUSD: 0.30, CacheHitPerMillionUSD: 0.014,
	},
	"deepseek-v3": {
		InputPerMillion: 0.50, OutputPerMillion: 2.19, CacheHitPerMillion: 0.10,
		InputPerMillionUSD: 0.07, OutputPerMillionUSD: 0.30, CacheHitPerMillionUSD: 0.014,
	},
	// DeepSeek-R1 (reasoning model)
	"deepseek-reasoner": {
		InputPerMillion: 0.55, OutputPerMillion: 8.19, CacheHitPerMillion: 0.14,
		InputPerMillionUSD: 0.076, OutputPerMillionUSD: 1.14, CacheHitPerMillionUSD: 0.019,
	},
	"deepseek-r1": {
		InputPerMillion: 0.55, OutputPerMillion: 8.19, CacheHitPerMillion: 0.14,
		InputPerMillionUSD: 0.076, OutputPerMillionUSD: 1.14, CacheHitPerMillionUSD: 0.019,
	},
}

// pricingSummary returns a formatted string of key model prices for display.
func pricingSummary() string {
	return "V3/chat: ¥0.50/¥2.19 (~$0.07/$0.30) · R1/reasoner: ¥0.55/¥8.19 (~$0.08/$1.14) " +
		"(input/output per 1M tokens; cache hits ~80% off)"
}

// balanceResponse is the JSON returned by GET /user/balance.
type balanceResponse struct {
	IsAvailable  bool          `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// Provider implements core.QuotaProvider for the DeepSeek API.
type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) ID() string { return "deepseek" }

func (p *Provider) Describe() core.ProviderInfo {
	return core.ProviderInfo{
		Name:         "DeepSeek",
		Capabilities: []string{"headers", "balance_endpoint"},
		DocURL:       "https://platform.deepseek.com/api-docs",
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

	// 1. Query /user/balance for credit info
	balanceURL := baseURL + "/user/balance"
	if err := p.fetchBalance(ctx, balanceURL, apiKey, &snap); err != nil {
		snap.Raw["balance_error"] = err.Error()
	}

	// 2. Query /v1/models for rate-limit headers
	modelsURL := baseURL + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, modelsURL, nil)
	if err != nil {
		return snap, fmt.Errorf("deepseek: creating models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		// If balance succeeded, return partial data
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, fmt.Errorf("deepseek: models request failed: %w", err)
	}
	defer resp.Body.Close()

	// Merge response headers into raw
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

	// Parse OpenAI-compatible rate-limit headers
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

func (p *Provider) fetchBalance(ctx context.Context, url, apiKey string, snap *core.QuotaSnapshot) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating balance request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("balance request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading balance body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("balance endpoint returned HTTP %d", resp.StatusCode)
	}

	var balResp balanceResponse
	if err := json.Unmarshal(body, &balResp); err != nil {
		return fmt.Errorf("parsing balance response: %w", err)
	}

	if balResp.IsAvailable {
		snap.Raw["account_available"] = "true"
	} else {
		snap.Raw["account_available"] = "false"
		snap.Status = core.StatusError
		snap.Message = "DeepSeek account is not available"
	}

	for _, info := range balResp.BalanceInfos {
		total, _ := strconv.ParseFloat(info.TotalBalance, 64)
		granted, _ := strconv.ParseFloat(info.GrantedBalance, 64)
		toppedUp, _ := strconv.ParseFloat(info.ToppedUpBalance, 64)

		unit := info.Currency
		if unit == "" {
			unit = "CNY"
		}

		snap.Metrics["total_balance"] = core.Metric{
			Remaining: &total,
			Unit:      unit,
			Window:    "current",
		}
		snap.Metrics["granted_balance"] = core.Metric{
			Remaining: &granted,
			Unit:      unit,
			Window:    "current",
		}
		snap.Metrics["topped_up_balance"] = core.Metric{
			Remaining: &toppedUp,
			Unit:      unit,
			Window:    "current",
		}

		snap.Raw["currency"] = unit
		snap.Raw["total_balance"] = info.TotalBalance
		snap.Raw["granted_balance"] = info.GrantedBalance
		snap.Raw["topped_up_balance"] = info.ToppedUpBalance

		if snap.Status == "" || snap.Status == core.StatusOK {
			snap.Status = core.StatusOK
			snap.Message = fmt.Sprintf("Balance: %s %s", info.TotalBalance, unit)
		}
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
