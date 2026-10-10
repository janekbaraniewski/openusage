package tui

import (
	"os"
	"strings"
	"sync"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

var (
	providerSpecsOnce sync.Once
	providerSpecs     map[string]core.ProviderSpec
	providerWidgets   map[string]core.DashboardWidget
	providerOrder     []string

	providerWidgetOverridesMu    sync.RWMutex
	providerSectionOrderOverride []core.DashboardStandardSection
	providerSectionOverrideSet   bool

	detailSectionOverridesMu   sync.RWMutex
	detailSectionOrderOverride []core.DetailStandardSection
	detailSectionOverrideSet   bool
)

func loadProviderSpecs() {
	providerSpecsOnce.Do(func() {
		providerSpecs = make(map[string]core.ProviderSpec)
		providerWidgets = make(map[string]core.DashboardWidget)
		for _, p := range providers.AllProviders() {
			spec := p.Spec()
			id := spec.ID
			if id == "" {
				id = p.ID()
			}
			providerSpecs[id] = spec
			providerWidgets[id] = p.DashboardWidget()
			providerOrder = append(providerOrder, id)
		}
	})
}

func dashboardWidget(providerID string) core.DashboardWidget {
	loadProviderSpecs()

	if cfg, ok := providerWidgets[providerID]; ok {
		return applyDashboardSectionOverride(cfg)
	}
	return applyDashboardSectionOverride(core.DefaultDashboardWidget())
}

type apiKeyProviderEntry struct {
	ProviderID string
	AccountID  string
}

var apiKeyEnvAliases = map[string][]string{
	"opencode":   {"ZEN_API_KEY"},
	"gemini_api": {"GOOGLE_API_KEY"},
	"zai":        {"ZHIPUAI_API_KEY"},
}

// localAuthEnvHints gives the AUTH SOURCE label for providers whose primary
// auth is not API-key based but which still accept an optional env
// credential. Muse Code reads local session logs with no key required;
// META_API_KEY (or the macOS keychain entry written by `muse login`) only
// feeds the optional quota probe.
var localAuthEnvHints = map[string]string{
	"muse_code": "META_API_KEY",
}

func localAuthEnvHintForProvider(providerID string) string {
	return strings.TrimSpace(localAuthEnvHints[providerID])
}

func apiKeyProviderEntries() []apiKeyProviderEntry {
	loadProviderSpecs()

	var entries []apiKeyProviderEntry
	for _, id := range providerOrder {
		spec := providerSpecs[id]
		if spec.Auth.Type != core.ProviderAuthTypeAPIKey {
			continue
		}
		envVar := spec.Auth.APIKeyEnv
		if envVar == "" {
			continue
		}
		accountID := spec.Auth.DefaultAccountID
		if accountID == "" {
			accountID = id
		}
		entries = append(entries, apiKeyProviderEntry{
			ProviderID: id,
			AccountID:  accountID,
		})
	}
	return entries
}

func isAPIKeyProvider(providerID string) bool {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return false
	}
	return spec.Auth.Type == core.ProviderAuthTypeAPIKey && spec.Auth.APIKeyEnv != ""
}

func envVarForProvider(providerID string) string {
	envVars := apiKeyEnvVarsForProvider(providerID)
	if len(envVars) == 0 {
		return ""
	}
	return envVars[0]
}

func apiKeyEnvVarsForProvider(providerID string) []string {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok || spec.Auth.Type != core.ProviderAuthTypeAPIKey || spec.Auth.APIKeyEnv == "" {
		return nil
	}

	envVars := []string{spec.Auth.APIKeyEnv}
	envVars = append(envVars, apiKeyEnvAliases[providerID]...)
	return dedupeNonEmptyStrings(envVars)
}

func apiKeyEnvLabelForProvider(providerID string) string {
	envVars := apiKeyEnvVarsForProvider(providerID)
	if len(envVars) == 0 {
		return ""
	}
	return strings.Join(envVars, " / ")
}

func resolvedAPIKeyEnvForProvider(providerID string) string {
	for _, envVar := range apiKeyEnvVarsForProvider(providerID) {
		if strings.TrimSpace(os.Getenv(envVar)) != "" {
			return envVar
		}
	}
	return envVarForProvider(providerID)
}

func hasConfiguredAPIKeyEnv(providerID string) bool {
	for _, envVar := range apiKeyEnvVarsForProvider(providerID) {
		if strings.TrimSpace(os.Getenv(envVar)) != "" {
			return true
		}
	}
	return false
}

// browserSessionProviderEntry is the analogue of apiKeyProviderEntry for
// providers whose PRIMARY auth path is a browser-session cookie. Used by the
// 5 KEYS tab to seed rows for declared providers even when the user has
// no account configured yet.
type browserSessionProviderEntry struct {
	ProviderID string
	AccountID  string
	Domain     string
	CookieName string
	ConsoleURL string
}

func browserSessionProviderEntries() []browserSessionProviderEntry {
	loadProviderSpecs()

	var entries []browserSessionProviderEntry
	for _, id := range providerOrder {
		spec := providerSpecs[id]
		if spec.Auth.Type != core.ProviderAuthTypeBrowserSession {
			continue
		}
		if spec.Auth.BrowserCookieDomain == "" || spec.Auth.BrowserCookieName == "" {
			// Spec is misdeclared — without a cookie ref we have no idea
			// what to extract. Skip rather than seed a broken row.
			continue
		}
		accountID := spec.Auth.DefaultAccountID
		if accountID == "" {
			accountID = id
		}
		consoleURL := spec.Auth.BrowserConsoleURL
		if consoleURL == "" {
			consoleURL = "https://" + strings.TrimPrefix(spec.Auth.BrowserCookieDomain, ".")
		}
		entries = append(entries, browserSessionProviderEntry{
			ProviderID: id,
			AccountID:  accountID,
			Domain:     spec.Auth.BrowserCookieDomain,
			CookieName: spec.Auth.BrowserCookieName,
			ConsoleURL: consoleURL,
		})
	}
	return entries
}

// isBrowserSessionProvider reports whether the provider's PRIMARY auth path
// is a browser-session cookie. These providers can be configured from the
// 5 KEYS tab even when no account exists yet (for example Perplexity).
func isBrowserSessionProvider(providerID string) bool {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return false
	}
	return spec.Auth.Type == core.ProviderAuthTypeBrowserSession
}

// supportsBrowserSessionProvider reports whether the provider supports a
// browser-session cookie as either its primary or a supplemental auth path.
// Used for mixed-auth rows like OpenCode where API-key config remains the
// primary path but console enrichment is available via browser session.
func supportsBrowserSessionProvider(providerID string) bool {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return false
	}
	return spec.Auth.SupportsAuth(core.ProviderAuthTypeBrowserSession)
}

// browserCookieRefForProvider returns the (domain, cookie_name, console_url)
// triple a provider declares for its browser-session auth path. Empty
// strings on the second + third components are valid only when the
// provider doesn't support browser-session auth.
func browserCookieRefForProvider(providerID string) (domain, cookieName, consoleURL string) {
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return "", "", ""
	}
	consoleURL = spec.Auth.BrowserConsoleURL
	if consoleURL == "" && spec.Auth.BrowserCookieDomain != "" {
		consoleURL = "https://" + strings.TrimPrefix(spec.Auth.BrowserCookieDomain, ".")
	}
	return spec.Auth.BrowserCookieDomain, spec.Auth.BrowserCookieName, consoleURL
}

func dedupeNonEmptyStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	var deduped []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		deduped = append(deduped, value)
	}
	return deduped
}

func setDashboardWidgetSectionOverrides(sections []core.DashboardStandardSection) {
	providerWidgetOverridesMu.Lock()
	defer providerWidgetOverridesMu.Unlock()

	if sections == nil {
		providerSectionOrderOverride = nil
		providerSectionOverrideSet = false
		return
	}

	seen := make(map[core.DashboardStandardSection]bool, len(sections))
	filtered := make([]core.DashboardStandardSection, 0, len(sections))
	for _, section := range sections {
		if !core.IsKnownDashboardStandardSection(section) || seen[section] {
			continue
		}
		filtered = append(filtered, section)
		seen[section] = true
	}
	providerSectionOrderOverride = append([]core.DashboardStandardSection(nil), filtered...)
	providerSectionOverrideSet = true
}

func setDetailSectionOverrides(sections []core.DetailStandardSection) {
	detailSectionOverridesMu.Lock()
	defer detailSectionOverridesMu.Unlock()

	if sections == nil {
		detailSectionOrderOverride = nil
		detailSectionOverrideSet = false
		return
	}

	seen := make(map[core.DetailStandardSection]bool, len(sections))
	filtered := make([]core.DetailStandardSection, 0, len(sections))
	for _, section := range sections {
		if !core.IsKnownDetailStandardSection(section) || seen[section] {
			continue
		}
		filtered = append(filtered, section)
		seen[section] = true
	}
	detailSectionOrderOverride = append([]core.DetailStandardSection(nil), filtered...)
	detailSectionOverrideSet = true
}

func effectiveDetailSectionOrder() []core.DetailStandardSection {
	detailSectionOverridesMu.RLock()
	sections := detailSectionOrderOverride
	set := detailSectionOverrideSet
	detailSectionOverridesMu.RUnlock()

	if !set || len(sections) == 0 {
		return core.DefaultDetailSectionOrder()
	}
	return append([]core.DetailStandardSection(nil), sections...)
}

func applyDashboardSectionOverride(cfg core.DashboardWidget) core.DashboardWidget {
	providerWidgetOverridesMu.RLock()
	sections := providerSectionOrderOverride
	set := providerSectionOverrideSet
	providerWidgetOverridesMu.RUnlock()

	if !set {
		return cfg
	}

	cfg.StandardSectionOrder = append([]core.DashboardStandardSection(nil), sections...)
	return cfg
}
