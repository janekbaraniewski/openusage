package deepseek

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/parsers"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

const (
	defaultBaseURL = "https://api.deepseek.com"
	modelsPath     = "/v1/models"
	balancePath    = "/user/balance"
)

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

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "deepseek",
			Info: core.ProviderInfo{
				Name:         "DeepSeek",
				Capabilities: []string{"headers", "balance_endpoint"},
				DocURL:       "https://platform.deepseek.com/api-docs",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "DEEPSEEK_API_KEY",
				DefaultAccountID: "deepseek",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{"Set DEEPSEEK_API_KEY to a valid DeepSeek API key."},
			},
			Dashboard: providerbase.DefaultDashboard(providerbase.WithColorRole(core.DashboardColorRoleSky)),
		}),
	}
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey := acct.ResolveAPIKey()
	if apiKey == "" {
		return core.UsageSnapshot{
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

	snap := core.UsageSnapshot{
		ProviderID: p.ID(),
		AccountID:  acct.ID,
		Timestamp:  time.Now(),
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
	}

	if err := p.fetchBalance(ctx, baseURL+balancePath, apiKey, &snap); err != nil {
		snap.Raw["balance_error"] = err.Error()
	}

	if err := p.fetchRateLimits(ctx, baseURL+modelsPath, apiKey, &snap); err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, err
	}

	if snap.Status == "" {
		snap.Status = core.StatusOK
		snap.Message = "OK"
	}

	return snap, nil
}

func (p *Provider) fetchBalance(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot) error {
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

	snap.Raw["account_available"] = strconv.FormatBool(balResp.IsAvailable)
	if !balResp.IsAvailable {
		snap.Status = core.StatusError
		snap.Message = "DeepSeek account is not available"
	}

	if len(balResp.BalanceInfos) == 0 {
		return nil
	}

	info := balResp.BalanceInfos[0]
	currency := info.Currency
	if currency == "" {
		currency = "CNY"
	}

	total, _ := strconv.ParseFloat(info.TotalBalance, 64)
	granted, _ := strconv.ParseFloat(info.GrantedBalance, 64)
	toppedUp, _ := strconv.ParseFloat(info.ToppedUpBalance, 64)

	snap.Metrics["total_balance"] = core.Metric{Remaining: &total, Unit: currency, Window: "current"}
	snap.Metrics["granted_balance"] = core.Metric{Remaining: &granted, Unit: currency, Window: "current"}
	snap.Metrics["topped_up_balance"] = core.Metric{Remaining: &toppedUp, Unit: currency, Window: "current"}

	snap.Raw["currency"] = currency
	snap.Raw["total_balance"] = info.TotalBalance
	snap.Raw["granted_balance"] = info.GrantedBalance
	snap.Raw["topped_up_balance"] = info.ToppedUpBalance

	if snap.Status == "" || snap.Status == core.StatusOK {
		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("Balance: %s %s", info.TotalBalance, currency)
	}

	return nil
}

func (p *Provider) fetchRateLimits(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating models request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("models request failed: %w", err)
	}
	defer resp.Body.Close()

	for k, v := range parsers.RedactHeaders(resp.Header) {
		snap.Raw[k] = v
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		snap.Status = core.StatusAuth
		snap.Message = fmt.Sprintf("HTTP %d – check API key", resp.StatusCode)
		return nil
	case http.StatusTooManyRequests:
		snap.Status = core.StatusLimited
		snap.Message = "rate limited (HTTP 429)"
	}

	parsers.ApplyRateLimitGroup(resp.Header, snap, "rpm", "requests", "1m",
		"x-ratelimit-limit-requests", "x-ratelimit-remaining-requests", "x-ratelimit-reset-requests")
	parsers.ApplyRateLimitGroup(resp.Header, snap, "tpm", "tokens", "1m",
		"x-ratelimit-limit-tokens", "x-ratelimit-remaining-tokens", "x-ratelimit-reset-tokens")

	return nil
}
