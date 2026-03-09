package core

import (
	"context"
	"os"
	"strings"
)

type AccountConfig struct {
	ID         string `json:"id"`
	Provider   string `json:"provider"`
	Auth       string `json:"auth,omitempty"`        // "api_key", "oauth", "cli", "local", "token"
	APIKeyEnv  string `json:"api_key_env,omitempty"` // env var name holding the API key
	ProbeModel string `json:"probe_model,omitempty"` // model to use for probe requests

	// Binary stores a CLI binary path for providers that execute a local command.
	// Provider-specific local data paths belong in Paths. Legacy Binary-based
	// data-path compatibility is handled inside the affected provider packages.
	Binary string `json:"binary,omitempty"`

	// BaseURL stores an HTTP API base URL for providers with configurable
	// endpoints. Provider-specific local data paths belong in Paths. Legacy
	// BaseURL-based data-path compatibility is handled inside provider packages.
	BaseURL string `json:"base_url,omitempty"`

	// Paths holds named provider-specific paths/URLs that are not part of the
	// shared account contract. Keys are provider-defined (for example
	// "tracking_db", "state_db", "stats_cache", "account_config").
	Paths map[string]string `json:"paths,omitempty"`

	Token     string            `json:"-"` // runtime-only: access token (never persisted)
	ExtraData map[string]string `json:"-"` // runtime-only: extra detection data (never persisted)
}

// Path returns the named provider-specific path. It checks Paths first,
// then ExtraData (for backward compat with detect), then the given fallback.
func (c AccountConfig) Path(key, fallback string) string {
	if c.Paths != nil {
		if v, ok := c.Paths[key]; ok && v != "" {
			return v
		}
	}
	if c.ExtraData != nil {
		if v, ok := c.ExtraData[key]; ok && v != "" {
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
	if c.Paths == nil {
		c.Paths = make(map[string]string)
	}
	c.Paths[key] = strings.TrimSpace(value)
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
