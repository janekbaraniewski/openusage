package deepseek

import (
	"context"
	"fmt"
	"strconv"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
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
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	snap := core.NewUsageSnapshot(p.ID(), acct.ID)

	if err := p.fetchBalance(ctx, baseURL+balancePath, apiKey, &snap); err != nil {
		snap.Raw["balance_error"] = err.Error()
	}

	if err := p.fetchRateLimits(ctx, baseURL+modelsPath, apiKey, &snap); err != nil {
		if snap.Status == core.StatusOK {
			return snap, nil
		}
		return snap, err
	}

	shared.FinalizeStatus(&snap)
	return snap, nil
}

func (p *Provider) fetchBalance(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot) error {
	var balResp balanceResponse
	if _, _, err := shared.FetchJSON(ctx, url, apiKey, &balResp, p.Client()); err != nil {
		return fmt.Errorf("balance: %w", err)
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
	return shared.ProbeRateLimits(ctx, url, apiKey, snap, p.Client())
}
