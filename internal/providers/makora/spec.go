package makora

import (
	"github.com/janekbaraniewski/openusage/internal/core"
)

const (
	// defaultBaseURL hosts all bearer-JWT data endpoints.
	defaultBaseURL = "https://app.makora.com"
	// baseLoginURL hosts the password/refresh auth endpoints.
	baseLoginURL = "https://be.prod.makora.com"

	statusPath  = "/api/v1/credits/status"
	userPath    = "/api/v1/credits/user"
	ratesPath   = "/api/v1/credits/default-rates"
	usagePrefix = "/api/v1/usage/my-usage/between/"
	loginPath   = "/api/v1/login/access-token"
	refreshPath = "/api/v1/login/refresh"
	deviceName  = "openusage"
)

// spec returns the canonical ProviderSpec for the makora provider.
func spec() core.ProviderSpec {
	return core.ProviderSpec{
		ID: "makora",
		Info: core.ProviderInfo{
			Name:         "Makora",
			Capabilities: []string{"balance_endpoint", "usage_endpoint", "per_model_breakdown", "daily_series"},
			DocURL:       "https://app.makora.com",
		},
		Auth: core.ProviderAuthSpec{
			Type:             core.ProviderAuthTypeToken,
			DefaultAccountID: "makora",
		},
		Setup: core.ProviderSetupSpec{
			DocsURL: "https://app.makora.com",
			Quickstart: []string{
				"Set MAKORA_SESSION_TOKEN to a dashboard session JWT (simplest),",
				"or set MAKORA_EMAIL + MAKORA_PASSWORD for password login,",
				"or let openusage reuse the token cache at ~/.config/makora-usage/state.json.",
				"Note: MAKORA_API_KEY (Inference+Optimize) is NOT accepted by the billing endpoints (HTTP 403).",
			},
		},
		Dashboard: dashboardWidget(),
		Detail:    detailWidget(),
		CreditMetrics: map[string]core.BalanceSemantics{
			"credit_balance": core.BalancePoint,
			"monthly_spend":  core.BalanceCumulative,
			"spend_limit":    core.BalanceLimit,
		},
	}
}
