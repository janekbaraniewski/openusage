// Package makora implements the Makora billing/usage provider.
//
// Makora's billing endpoints are gated behind a dashboard *session JWT*
// (not the Inference+Optimize API key — that returns 403 on credits/usage).
// This provider resolves a session token in precedence order:
//
//  1. MAKORA_SESSION_TOKEN env var (manually pasted JWT)
//  2. ~/.config/makora-usage/state.json token cache (read-only sibling of the
//     shared makora-usage helper; may be refreshed when this flow logs in)
//  3. MAKORA_EMAIL + MAKORA_PASSWORD env vars → password login
//
// Data endpoints (base https://app.makora.com, "Authorization: Bearer <jwt>"):
//
//	GET /api/v1/credits/status                 → live balance
//	GET /api/v1/credits/user                   → monthly spend limit / auto-recharge
//	GET /api/v1/usage/my-usage/between/{start}/{end}?paygo=true → per-model daily usage
//	GET /api/v1/credits/default-rates          → public per-1M-token rate card
//
// cached_tokens is a SUBSET of input_tokens; non-cached input is
// input_tokens − cached_tokens. Cost per model-day row uses the public rate
// card: (input−cached)·prompt + completion·completion + cached·cache_read.
package makora

import (
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
)

// Provider implements core.UsageProvider for Makora.
type Provider struct {
	providerbase.Base
}

// New returns the provider with its fully-populated spec and widgets.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(spec()),
	}
}
