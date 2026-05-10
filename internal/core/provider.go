package core

import (
	"context"
	"os"
	"strings"
	"time"
)

type AccountConfig struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Auth       string `json:"auth,omitempty"`        // "api_key", "oauth", "cli", "local", "token", "browser_session"
	APIKeyEnv  string `json:"api_key_env,omitempty"` // env var name holding the API key
	ProbeModel string `json:"probe_model,omitempty"` // model to use for probe requests

	// BrowserCookie identifies the (domain, cookie_name, source_browser)
	// triple used for browser-session-auth providers. Persisted alongside
	// the account config. The actual cookie value is never stored here —
	// it lives in the 0o600 credentials store, keyed by account ID.
	// See docs/BROWSER_SESSION_AUTH_DESIGN.md.
	BrowserCookie *BrowserCookieRef `json:"browser_cookie,omitempty"`

	// Binary stores a CLI binary path for providers that execute a local command.
	// Provider-specific local data paths belong in ProviderPaths. Legacy Binary-based
	// data-path compatibility is handled inside the affected provider packages.
	Binary string `json:"binary,omitempty"`

	// BaseURL stores an HTTP API base URL for providers with configurable
	// endpoints. Provider-specific local data paths belong in ProviderPaths. Legacy
	// BaseURL-based data-path compatibility is handled inside provider packages.
	BaseURL string `json:"base_url,omitempty"`

	// ProviderPaths holds named provider-specific paths/URLs that are not part
	// of the shared account contract. Keys are provider-defined (for example
	// "tracking_db", "state_db", "stats_cache", "account_config").
	ProviderPaths map[string]string `json:"provider_paths,omitempty"`

	// Paths is a legacy persisted alias for provider-specific paths. New code
	// should use ProviderPaths through Path/SetPath helpers.
	Paths map[string]string `json:"paths,omitempty"`

	Token        string            `json:"-"` // runtime-only: access token (never persisted)
	RuntimeHints map[string]string `json:"-"` // runtime-only: detection metadata + local hints (never persisted)
}

// Path returns the named provider-specific path. It checks ProviderPaths
// first, then the legacy Paths field, then RuntimeHints (which detectors use
// for transient locators), and finally the caller's fallback.
func (c AccountConfig) Path(key, fallback string) string {
	if c.ProviderPaths != nil {
		if v, ok := c.ProviderPaths[key]; ok && v != "" {
			return v
		}
	}
	if c.Paths != nil {
		if v, ok := c.Paths[key]; ok && v != "" {
			return v
		}
	}
	if c.RuntimeHints != nil {
		if v, ok := c.RuntimeHints[key]; ok && v != "" {
			return v
		}
	}
	if fallback != "" {
		return fallback
	}
	return ""
}

// SetPath stores a named provider-specific path.
func (c *AccountConfig) SetPath(key, value string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	if c.ProviderPaths == nil {
		c.ProviderPaths = make(map[string]string)
	}
	c.ProviderPaths[key] = strings.TrimSpace(value)
}

func (c AccountConfig) Hint(key, fallback string) string {
	if c.RuntimeHints != nil {
		if v, ok := c.RuntimeHints[key]; ok && v != "" {
			return v
		}
	}
	if fallback != "" {
		return fallback
	}
	return ""
}

func (c *AccountConfig) SetHint(key, value string) {
	if c == nil || strings.TrimSpace(key) == "" || strings.TrimSpace(value) == "" {
		return
	}
	if c.RuntimeHints == nil {
		c.RuntimeHints = make(map[string]string)
	}
	c.RuntimeHints[strings.TrimSpace(key)] = strings.TrimSpace(value)
}

// PathMap returns a merged copy of provider-local paths, preferring
// ProviderPaths over legacy Paths.
func (c AccountConfig) PathMap() map[string]string {
	if len(c.ProviderPaths) == 0 && len(c.Paths) == 0 {
		return nil
	}
	out := make(map[string]string, len(c.ProviderPaths)+len(c.Paths))
	for key, value := range c.Paths {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	for key, value := range c.ProviderPaths {
		trimmedKey := strings.TrimSpace(key)
		trimmedValue := strings.TrimSpace(value)
		if trimmedKey == "" || trimmedValue == "" {
			continue
		}
		out[trimmedKey] = trimmedValue
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (c AccountConfig) ResolveAPIKey() string {
	if c.Token != "" {
		return c.Token
	}
	return os.Getenv(c.APIKeyEnv)
}

type ProviderInfo struct {
	Name         string   // e.g. "OpenAI", "Anthropic"
	Capabilities []string // "headers", "cli_stats", "usage_endpoint", "credits_endpoint"
	DocURL       string   // link to vendor's rate-limit documentation
}

type UsageProvider interface {
	ID() string

	Describe() ProviderInfo

	// Spec defines provider-level auth/setup metadata and presentation defaults.
	Spec() ProviderSpec

	// DashboardWidget defines how provider metrics should be presented in dashboard tiles.
	DashboardWidget() DashboardWidget
	// DetailWidget defines how sections should be rendered in the details panel.
	DetailWidget() DetailWidget

	Fetch(ctx context.Context, acct AccountConfig) (UsageSnapshot, error)
}

// ChangeDetector is an optional interface that UsageProvider implementations
// may implement to skip expensive Fetch() calls when data hasn't changed.
// Implementations should be cheap (stat() calls, not file reads).
// On error, callers assume changed=true (safe fallback).
type ChangeDetector interface {
	HasChanged(acct AccountConfig, since time.Time) (bool, error)
}
