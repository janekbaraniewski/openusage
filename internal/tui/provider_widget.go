package tui

import (
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
	EnvVar     string
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
			EnvVar:     envVar,
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
	loadProviderSpecs()
	spec, ok := providerSpecs[providerID]
	if !ok {
		return ""
	}
	return spec.Auth.APIKeyEnv
}

// browserSessionProviderEntry is the analogue of apiKeyProviderEntry for
// providers that authenticate via browser-session cookies. Used by the
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
		if !spec.Auth.SupportsAuth(core.ProviderAuthTypeBrowserSession) {
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

// isBrowserSessionProvider reports whether the given provider supports
// browser-session auth as a primary or supplemental credential path. Used
// by the 5 KEYS tab to decide whether to render the "Connect via browser"
// row + handle the connect modal flow.
func isBrowserSessionProvider(providerID string) bool {
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
