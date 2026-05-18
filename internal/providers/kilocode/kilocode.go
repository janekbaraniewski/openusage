// Package kilocode implements a local-data provider for the Kilo Code
// VS Code extension. Kilo Code stores per-task usage events under VS Code
// globalStorage using the exact schema Roo Code uses, so the heavy lifting
// (event parsing, multi-VS-Code-variant path discovery, cross-variant
// dedup) is delegated to the shared roocode package; this file holds only
// the extension-specific glue.
package kilocode

import (
	"context"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/providerbase"
	"github.com/janekbaraniewski/openusage/internal/providers/roocode"
)

// ID is the canonical provider identifier registered in the providers
// registry.
const ID = "kilo_code"

// DefaultAccountID is the account ID used by the auto-detector when it
// registers a local Kilo Code install.
const DefaultAccountID = "kilo_code"

// Provider implements core.UsageProvider for Kilo Code by delegating to
// the shared roocode package with the Kilo Code extension subdirectory
// and client identifier.
type Provider struct {
	providerbase.Base
	clock core.Clock
}

// New constructs a Kilo Code provider.
func New() *Provider {
	return &Provider{
		Base: providerbase.New(core.ProviderSpec{
			ID: ID,
			Info: core.ProviderInfo{
				Name:         "Kilo Code",
				Capabilities: []string{"local_stats", "session_tracking", "model_tokens", "cost_estimation"},
				DocURL:       "https://github.com/Kilo-Org/kilocode",
			},
			Auth: core.ProviderAuthSpec{
				Type:             core.ProviderAuthTypeLocal,
				DefaultAccountID: DefaultAccountID,
			},
			Setup: core.ProviderSetupSpec{
				Quickstart: []string{
					"Install the Kilo Code VS Code extension and run at least one task.",
					"openusage discovers the extension's task logs from VS Code globalStorage; no configuration required.",
				},
			},
			Dashboard: roocode.DashboardWidget(core.DashboardColorRoleMauve),
		}),
		clock: core.SystemClock{},
	}
}

// DetailWidget returns the standard coding-tool detail layout.
func (p *Provider) DetailWidget() core.DetailWidget {
	return core.CodingToolDetailWidget(false)
}

// HasChanged delegates to the shared extension-change detector.
func (p *Provider) HasChanged(acct core.AccountConfig, since time.Time) (bool, error) {
	return roocode.ExtensionChanged(roocode.KiloExtensionSubdir, since), nil
}

// Fetch enumerates Kilo Code's per-task directories across every VS Code
// variant, parses them with the shared roocode parser, and aggregates the
// result into a UsageSnapshot.
func (p *Provider) Fetch(ctx context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	if strings.TrimSpace(acct.Provider) == "" {
		acct.Provider = p.ID()
	}
	clock := p.clock
	if clock == nil {
		clock = core.SystemClock{}
	}
	return roocode.FetchExtension(ctx, p.ID(), acct, roocode.KiloExtensionSubdir, roocode.ClientKiloCode, "Kilo Code", clock)
}
