// Package moonshot implements the Moonshot AI (Kimi) usage provider.
//
// Two services exist:
//   - api.moonshot.ai (international, USD)        — default
//   - api.moonshot.cn (China mainland, CNY)       — opt-in via BaseURL override
//
// Both expose the same endpoint shape. Auth is "Authorization: Bearer <key>".
//
// Two endpoints carry the data we surface:
//
//	GET /v1/users/me            — org limits, tier, ids
//	GET /v1/users/me/balance    — balance breakdown (available / voucher / cash)
//
// Per-model usage and historical daily series are not exposed by the API.
// Those signals populate from the telemetry pipeline when matching events
// (e.g. provider_id=moonshot from OpenCode hooks) are available.
package moonshot

import (
	"context"
	"fmt"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const (
	defaultBaseURL = "https://api.moonshot.ai"
	cnBaseURL      = "https://api.moonshot.cn"
	userInfoPath   = "/v1/users/me"
	balancePath    = "/v1/users/me/balance"
)

type Provider struct {
	providerbase.Base
}

func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: "moonshot",
			Info: core.ProviderInfo{
				Name:         "Moonshot",
				Capabilities: []string{"balance_endpoint", "user_info_endpoint"},
				DocURL:       "https://platform.moonshot.ai/docs/api/",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeAPIKey,
				APIKeyEnv:        "MOONSHOT_API_KEY",
				DefaultAccountID: "moonshot-ai",
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Set MOONSHOT_API_KEY to a key from https://platform.moonshot.ai/console/api-keys.",
					"For Moonshot.cn (China), add a second account in settings.json with base_url https://api.moonshot.cn.",
				},
			},
			Dashboard: dashboardWidget(),
		}),
	}
}

type userInfoResponse struct {
	Code    int    `json:"code"`
	SCode   string `json:"scode"`
	Status  bool   `json:"status"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    struct {
		AccessKey struct {
			ID string `json:"id"`
		} `json:"access_key"`
		Organization struct {
			ID                  string `json:"id"`
			MaxConcurrency      int    `json:"max_concurrency"`
			MaxRequestPerMinute int    `json:"max_request_per_minute"`
			MaxTokenPerMinute   int    `json:"max_token_per_minute"`
			MaxTokenQuota       int64  `json:"max_token_quota"`
		} `json:"organization"`
		Project struct {
			ID string `json:"id"`
		} `json:"project"`
		User struct {
			ID          string `json:"id"`
			UserGroupID string `json:"user_group_id"`
			UserState   string `json:"user_state"`
		} `json:"user"`
		UserGroupID string `json:"user_group_id"`
	} `json:"data"`
}

type balanceResponse struct {
	Code    int    `json:"code"`
	SCode   string `json:"scode"`
	Status  bool   `json:"status"`
	Error   string `json:"error,omitempty"`
	Message string `json:"message,omitempty"`
	Data    struct {
		AvailableBalance float64 `json:"available_balance"`
		VoucherBalance   float64 `json:"voucher_balance"`
		CashBalance      float64 `json:"cash_balance"`
	} `json:"data"`
}

func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	apiKey, authSnap := shared.RequireAPIKey(acct, p.ID())
	if authSnap != nil {
		return *authSnap, nil
	}

	baseURL := shared.ResolveBaseURL(acct, defaultBaseURL)
	region, currency := classifyService(baseURL)

	snap := core.NewUsageSnapshot(p.ID(), acct.ID)
	snap.SetAttribute("service_region", region)
	snap.SetAttribute("currency", currency)

	if err := p.fetchUserInfo(ctx, baseURL+userInfoPath, apiKey, &snap); err != nil {
		// fetchUserInfo sets snap.Status for terminal cases (auth/limited). For
		// transport errors it returns the error and we surface it but keep going
		// so a partial balance read still gives the user something.
		snap.Raw["user_info_error"] = err.Error()
		if snap.Status == core.StatusAuth {
			return snap, nil
		}
	}

	if err := p.fetchBalance(ctx, baseURL+balancePath, apiKey, &snap); err != nil {
		snap.Raw["balance_error"] = err.Error()
		// Do not overwrite a terminal Auth status; otherwise leave whatever
		// the user-info call set.
	}

	applyBalanceStatus(&snap, currency)
	shared.FinalizeStatus(&snap)
	return snap, nil
}

func (p *Provider) fetchUserInfo(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot) error {
	var info userInfoResponse
	statusCode, _, err := shared.FetchJSON(ctx, url, apiKey, &info, p.Client())
	if err != nil {
		shared.ApplyStatusFromCode(statusCode, snap, "MOONSHOT_API_KEY")
		if snap.Status != "" {
			return nil
		}
		return fmt.Errorf("user info: %w", err)
	}
	if !info.Status || info.Code != 0 {
		return fmt.Errorf("user info api error: %s", firstNonEmpty(info.Message, info.Error, "unknown"))
	}

	d := info.Data
	if d.Organization.MaxRequestPerMinute > 0 {
		limit := float64(d.Organization.MaxRequestPerMinute)
		snap.Metrics["rpm"] = core.Metric{Limit: &limit, Unit: "requests", Window: "1m"}
	}
	if d.Organization.MaxTokenPerMinute > 0 {
		limit := float64(d.Organization.MaxTokenPerMinute)
		snap.Metrics["tpm"] = core.Metric{Limit: &limit, Unit: "tokens", Window: "1m"}
	}
	if d.Organization.MaxConcurrency > 0 {
		limit := float64(d.Organization.MaxConcurrency)
		snap.Metrics["concurrency_max"] = core.Metric{Limit: &limit, Unit: "requests", Window: "current"}
	}
	if d.Organization.MaxTokenQuota > 0 {
		limit := float64(d.Organization.MaxTokenQuota)
		snap.Metrics["total_token_quota"] = core.Metric{Limit: &limit, Unit: "tokens", Window: "current"}
	}

	if tier := firstNonEmpty(d.UserGroupID, d.User.UserGroupID); tier != "" {
		snap.SetAttribute("account_tier", tier)
	}
	if state := strings.TrimSpace(d.User.UserState); state != "" {
		snap.SetAttribute("user_state", state)
	}
	if id := strings.TrimSpace(d.Organization.ID); id != "" {
		snap.SetAttribute("org_id", id)
	}
	if id := strings.TrimSpace(d.Project.ID); id != "" {
		snap.SetAttribute("project_id", id)
	}
	if k := strings.TrimSpace(d.AccessKey.ID); k != "" {
		snap.SetAttribute("access_key_suffix", lastN(k, 4))
	}

	return nil
}

func (p *Provider) fetchBalance(ctx context.Context, url, apiKey string, snap *core.UsageSnapshot) error {
	var bal balanceResponse
	statusCode, _, err := shared.FetchJSON(ctx, url, apiKey, &bal, p.Client())
	if err != nil {
		// Don't clobber a status set by a previous fetch in the same poll.
		if snap.Status == "" {
			shared.ApplyStatusFromCode(statusCode, snap, "MOONSHOT_API_KEY")
		}
		if snap.Status == core.StatusAuth || snap.Status == core.StatusLimited {
			return nil
		}
		return fmt.Errorf("balance: %w", err)
	}
	if !bal.Status || bal.Code != 0 {
		return fmt.Errorf("balance api error: %s", firstNonEmpty(bal.Message, bal.Error, "unknown"))
	}

	currency := snap.Attributes["currency"]
	if currency == "" {
		currency = "USD"
	}

	available := bal.Data.AvailableBalance
	voucher := bal.Data.VoucherBalance
	cash := bal.Data.CashBalance

	// Moonshot's API only returns the currently-remaining balance — there's
	// no lifetime-deposit or lifetime-spend field. To render gauges with a
	// real denominator we persist a per-account high-water-mark of each
	// balance dimension and use that as the Limit. A new top-up bumps the
	// peak; spend-down then fills the gauge between Limit and Remaining.
	peaks := updatePeaks(snap.AccountID, peakState{
		PeakAvailable: available,
		PeakCash:      cash,
		PeakVoucher:   voucher,
	})

	snap.Metrics["available_balance"] = balanceMetric(peaks.PeakAvailable, available, currency)
	snap.Metrics["cash_balance"] = balanceMetric(peaks.PeakCash, cash, currency)
	snap.Metrics["voucher_balance"] = balanceMetric(peaks.PeakVoucher, voucher, currency)

	return nil
}

// balanceMetric builds a fully-populated balance Metric from a persisted peak
// (Limit) and the current remaining value. Used = Limit - Remaining is the
// implicit spend since the peak. When peak == 0 (first poll, account never
// observed in this state file) we still set Limit so the gauge shows full;
// the peak is simultaneously updated so subsequent polls have proper data.
func balanceMetric(peak, remaining float64, currency string) core.Metric {
	limit := peak
	if limit < remaining {
		limit = remaining
	}
	used := limit - remaining
	if used < 0 {
		used = 0
	}
	return core.Metric{
		Limit:     core.Float64Ptr(limit),
		Remaining: core.Float64Ptr(remaining),
		Used:      core.Float64Ptr(used),
		Unit:      currency,
		Window:    "current",
	}
}

// applyBalanceStatus promotes Status / Message based on remaining available balance.
// Existing terminal statuses (Auth, Limited, Error set by fetchers) are preserved.
func applyBalanceStatus(snap *core.UsageSnapshot, currency string) {
	if snap.Status != "" && snap.Status != core.StatusOK {
		return
	}
	bal, ok := snap.Metrics["available_balance"]
	if !ok || bal.Remaining == nil {
		return
	}
	avail := *bal.Remaining

	switch {
	case avail <= 0:
		snap.Status = core.StatusLimited
		snap.Message = "balance exhausted"
	case avail < 1.0:
		snap.Status = core.StatusNearLimit
		snap.Message = fmt.Sprintf("Low balance: %.2f %s", avail, currency)
	default:
		snap.Status = core.StatusOK
		snap.Message = fmt.Sprintf("Balance: %.2f %s", avail, currency)
	}
}

// classifyService maps a base URL to a (region, currency) pair. .cn → China/CNY,
// otherwise treated as the international service in USD.
func classifyService(baseURL string) (region, currency string) {
	if strings.Contains(baseURL, ".moonshot.cn") {
		return "china", "CNY"
	}
	return "international", "USD"
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
