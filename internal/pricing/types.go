// Package pricing fetches public model pricing data, caches it on disk,
// and resolves model identifiers (with fuzzy / tiered logic) to per-token
// rates. Public pricing sources used:
//
//   - LiteLLM model_prices_and_context_window.json (preferred, richest)
//   - OpenRouter /api/v1/models (fallback / cross-check)
//   - Hardcoded last-resort table (a small set of common models)
//
// All rates are normalised to USD per 1,000,000 tokens.
package pricing

import (
	"time"
)

// Source identifies which upstream produced a Price.
type Source string

const (
	SourceLiteLLM    Source = "litellm"
	SourceOpenRouter Source = "openrouter"
	SourceHardcoded  Source = "hardcoded"
	SourceCache      Source = "cache"
)

// Price represents the resolved per-million-token rates for a single model.
// All numeric fields are USD per 1,000,000 tokens. Optional rates are zero
// when the upstream did not publish them.
type Price struct {
	// ModelID is the canonical upstream identifier the price was resolved
	// against (after normalisation / fuzzy matching).
	ModelID string `json:"model_id"`
	// Provider is the upstream provider name where known (e.g. "anthropic",
	// "openai", "google"). Empty when unknown.
	Provider string `json:"provider,omitempty"`
	// Source identifies which upstream the price came from.
	Source Source `json:"source"`
	// ContextWindow is the published max input tokens, when known.
	ContextWindow int `json:"context_window,omitempty"`
	// LastUpdated is the time the upstream data was fetched / refreshed.
	LastUpdated time.Time `json:"last_updated"`

	// InputCostPerMillion is the prompt / input token rate (USD per 1M).
	InputCostPerMillion float64 `json:"input_cost_per_million"`
	// OutputCostPerMillion is the completion / output token rate.
	OutputCostPerMillion float64 `json:"output_cost_per_million"`
	// CacheReadCostPerMillion is the cached-read token rate (when supported).
	CacheReadCostPerMillion float64 `json:"cache_read_cost_per_million,omitempty"`
	// CacheWriteCostPerMillion is the cache-write / cache-create token rate.
	CacheWriteCostPerMillion float64 `json:"cache_write_cost_per_million,omitempty"`
	// ReasoningCostPerMillion is the reasoning-token rate, when distinct
	// from output. Most models bill reasoning at the output rate; this is
	// only set when the upstream publishes a separate rate.
	ReasoningCostPerMillion float64 `json:"reasoning_cost_per_million,omitempty"`

	// Tiers carries the optional per-context-window rate overrides
	// (>128k, >200k, >256k, >272k). nil rates mean "fall through to base".
	Tiers TierOverrides `json:"tiers,omitempty"`
}

// TierOverrides captures rate overrides that apply once the input length
// crosses a known threshold. Currently the canonical thresholds published
// by upstreams are 128k, 200k, 256k, and 272k tokens.
type TierOverrides struct {
	Above128k *TierRates `json:"above_128k,omitempty"`
	Above200k *TierRates `json:"above_200k,omitempty"`
	Above256k *TierRates `json:"above_256k,omitempty"`
	Above272k *TierRates `json:"above_272k,omitempty"`
}

// TierRates is the per-1M rates that override the base rates above a
// particular context-length threshold.
type TierRates struct {
	InputCostPerMillion      *float64 `json:"input_cost_per_million,omitempty"`
	OutputCostPerMillion     *float64 `json:"output_cost_per_million,omitempty"`
	CacheReadCostPerMillion  *float64 `json:"cache_read_cost_per_million,omitempty"`
	CacheWriteCostPerMillion *float64 `json:"cache_write_cost_per_million,omitempty"`
}

// TieredPrice pairs a single Price snapshot with the lower-bound context
// length at which its rates apply. Resolvers build a tier ladder by
// sorting TieredPrice entries by AppliesAbove ascending.
type TieredPrice struct {
	// AppliesAbove is the (exclusive) lower context-length bound at which
	// the contained Price takes effect. 0 means "base / no minimum".
	AppliesAbove int
	Price        Price
}
