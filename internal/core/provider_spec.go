package core

import "strings"

type ProviderAuthType string

const (
	ProviderAuthTypeUnknown ProviderAuthType = ""
	ProviderAuthTypeAPIKey  ProviderAuthType = "api_key"
	ProviderAuthTypeOAuth   ProviderAuthType = "oauth"
	ProviderAuthTypeCLI     ProviderAuthType = "cli"
	ProviderAuthTypeLocal   ProviderAuthType = "local"
	ProviderAuthTypeToken   ProviderAuthType = "token"
	// ProviderAuthTypeBrowserSession means the provider authenticates via a
	// session cookie extracted from the user's logged-in browser. Used for
	// dashboard-gated providers that don't accept bearer tokens for billing /
	// usage / account endpoints (OpenAI platform, Anthropic console, OpenCode
	// console, Perplexity console, Google AI Studio). Per design doc
	// docs/BROWSER_SESSION_AUTH_DESIGN.md.
	ProviderAuthTypeBrowserSession ProviderAuthType = "browser_session"
)

// BrowserCookieRef points at the (domain, cookie name) pair openusage should
// extract from the user's browser to authenticate console-API requests for a
// provider. The actual cookie value is never persisted in this struct — it
// lives in the credentials store and is re-extracted from the browser on
// every poll.
type BrowserCookieRef struct {
	// Domain the cookie is set on (e.g. ".opencode.ai", ".perplexity.ai").
	// Leading dot is optional; matchers normalize.
	Domain string `json:"domain,omitempty"`
	// CookieName is the literal cookie name (e.g. "auth",
	// "__Secure-next-auth.session-token").
	CookieName string `json:"cookie_name,omitempty"`
	// SourceBrowser is the browser the cookie was last extracted from
	// ("chrome", "firefox", "safari", "edge", "brave"). Auto-discovered on
	// first connect; persisted so subsequent polls go straight to that
	// browser instead of probing all.
	SourceBrowser string `json:"source_browser,omitempty"`
}

// ProviderAuthSpec defines how a provider authenticates and how users configure it.
type ProviderAuthSpec struct {
	Type             ProviderAuthType
	APIKeyEnv        string
	DefaultAccountID string

	// SupplementalTypes lists additional auth methods the provider can use
	// alongside Type. Most providers leave this nil — only relevant when a
	// provider supports multiple credential paths (e.g. OpenCode supports
	// both API-key probe of the Zen surface AND a richer browser-session
	// probe of its console RPCs).
	SupplementalTypes []ProviderAuthType

	// BrowserCookieDomain / BrowserCookieName describe the cookie the
	// provider's browser-session auth path reads. Required when Type or
	// SupplementalTypes contain ProviderAuthTypeBrowserSession.
	BrowserCookieDomain string
	BrowserCookieName   string

	// BrowserConsoleURL is the URL openusage opens in the user's default
	// browser when they click "Connect via browser" for this provider.
	// Optional — falls back to "https://" + BrowserCookieDomain if empty.
	BrowserConsoleURL string
}

// SupportsAuth reports whether the provider's auth spec accepts the given
// auth type as either its primary or a supplemental credential path.
func (a ProviderAuthSpec) SupportsAuth(t ProviderAuthType) bool {
	if a.Type == t {
		return true
	}
	for _, s := range a.SupplementalTypes {
		if s == t {
			return true
		}
	}
	return false
}

// BrowserSessionInfo summarises a stored browser-session credential without
// exposing the cookie value. Lives in core so both the daemon's
// service-layer and the TUI can reference it without circular imports.
type BrowserSessionInfo struct {
	Connected     bool
	Domain        string
	CookieName    string
	SourceBrowser string
	CapturedAt    string
	ExpiresAt     string
	Expired       bool
}

// ProviderSetupSpec describes setup entry points and quickstart instructions.
type ProviderSetupSpec struct {
	DocsURL    string
	Quickstart []string
}

// ProviderSpec is the canonical provider definition used for registration and UI metadata.
type ProviderSpec struct {
	ID        string
	Info      ProviderInfo
	Auth      ProviderAuthSpec
	Setup     ProviderSetupSpec
	Dashboard DashboardWidget
	Detail    DetailWidget

	// CreditMetrics declares, per money-metric key, how that metric's numeric
	// value behaves over time. The daemon records these metrics into a durable
	// observation series and derives true windowed spend from the deltas, so it
	// must know whether a value is a monotonic counter, a draining balance, or a
	// fixed cap. Leave empty for providers that expose no money metrics.
	CreditMetrics map[string]BalanceSemantics

	// Options declares the provider-specific configuration knobs this
	// provider understands (AccountConfig.Options keys), so the settings UI
	// and validation can be generated rather than hand-maintained.
	// Empty for providers that need nothing.
	Options []ProviderOption
}

// ProviderOption describes one provider-specific configuration knob: the
// AccountConfig.Options key, how the settings UI labels it, and whether it
// is required. Values stay stringly-typed; Required is the only validation.
type ProviderOption struct {
	Key         string // e.g. "gh_host"
	Label       string // "GitHub host", shown in settings
	Placeholder string // "my-org.ghe.com"
	Help        string // one line
	Required    bool
}

// BalanceSemantics classifies how a money metric's value moves over time, which
// determines how spend within a window is derived from observations of it.
type BalanceSemantics string

const (
	// BalanceCumulative is a monotonically increasing used/spent counter
	// (e.g. OpenRouter total_usage). Windowed spend = used(now) − used(window start).
	BalanceCumulative BalanceSemantics = "cumulative"
	// BalancePoint is a point-in-time remaining balance that falls as you spend
	// and rises on top-up (e.g. Moonshot available_balance). Windowed spend =
	// sum of the per-step drops, excluding top-ups.
	BalancePoint BalanceSemantics = "balance"
	// BalanceLimit is a fixed budget cap that carries no spend signal of its own
	// (e.g. Cursor spend_limit). No windowed spend is derived from it.
	BalanceLimit BalanceSemantics = "limit"
)

// InferBalanceSemantics returns the semantics declared for metricKey, falling
// back to an inference from the metric's Window when the provider has not
// declared it: a "lifetime"/"all-time" window implies a cumulative counter, a
// "current" balance implies a point-in-time balance. Returns ("", false) when
// nothing can be determined, so callers skip the metric.
func (s ProviderSpec) InferBalanceSemantics(metricKey, window string) (BalanceSemantics, bool) {
	if sem, ok := s.CreditMetrics[metricKey]; ok {
		return sem, true
	}
	switch strings.ToLower(strings.TrimSpace(window)) {
	case "lifetime", "all-time", "all", "alltime":
		return BalanceCumulative, true
	case "current":
		return BalancePoint, true
	}
	return "", false
}
