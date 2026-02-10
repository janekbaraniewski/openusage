package core

import "context"

// AccountConfig holds the per-account configuration loaded from TOML.
type AccountConfig struct {
	ID         string            `toml:"id"`
	Provider   string            `toml:"provider"`
	Auth       string            `toml:"auth,omitempty"`        // "api_key", "oauth", "cli", "local", "token"
	APIKeyEnv  string            `toml:"api_key_env,omitempty"` // env var name holding the API key
	ProbeModel string            `toml:"probe_model,omitempty"` // model to use for probe requests
	Binary     string            `toml:"binary,omitempty"`      // path to CLI binary
	BaseURL    string            `toml:"base_url,omitempty"`    // custom API base URL (e.g. for OpenRouter)
	Token      string            `toml:"-"`                     // runtime-only: access token (never persisted)
	ExtraData  map[string]string `toml:"-"`                     // runtime-only: extra detection data
}

// ProviderInfo describes a provider adapter's capabilities.
type ProviderInfo struct {
	Name         string   // e.g. "OpenAI", "Anthropic"
	Capabilities []string // "headers", "cli_stats", "usage_endpoint", "credits_endpoint"
	DocURL       string   // link to vendor's rate-limit documentation
}

// QuotaProvider is the interface every provider adapter must implement.
type QuotaProvider interface {
	// ID returns the unique provider identifier (e.g. "openai", "anthropic").
	ID() string

	// Describe returns metadata about this provider.
	Describe() ProviderInfo

	// Fetch probes the provider and returns the current quota snapshot.
	Fetch(ctx context.Context, acct AccountConfig) (QuotaSnapshot, error)
}
