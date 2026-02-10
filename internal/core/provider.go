package core

import (
	"context"
	"os"
)

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

type QuotaProvider interface {
	ID() string

	Describe() ProviderInfo

	Fetch(ctx context.Context, acct AccountConfig) (QuotaSnapshot, error)
}
