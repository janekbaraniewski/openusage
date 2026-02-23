package main

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFilterAccountsByDashboardConfig_DefaultEnabled(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "openrouter", Provider: "openrouter"},
		{ID: "codex-cli", Provider: "codex"},
	}

	filtered := filterAccountsByDashboardConfig(accounts, config.DashboardConfig{})
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
}

func TestFilterAccountsByDashboardConfig_ExcludesDisabled(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "openrouter", Provider: "openrouter"},
		{ID: "codex-cli", Provider: "codex"},
		{ID: "claude-code", Provider: "claude_code"},
	}

	filtered := filterAccountsByDashboardConfig(accounts, config.DashboardConfig{
		Providers: []config.DashboardProviderConfig{
			{AccountID: "codex-cli", Enabled: false},
			{AccountID: "openrouter", Enabled: true},
		},
	})

	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
	if filtered[0].ID != "openrouter" {
		t.Fatalf("filtered[0] = %q, want openrouter", filtered[0].ID)
	}
	if filtered[1].ID != "claude-code" {
		t.Fatalf("filtered[1] = %q, want claude-code", filtered[1].ID)
	}
}

func TestDisabledAccountsFromDashboard(t *testing.T) {
	disabled := disabledAccountsFromDashboard(config.DashboardConfig{
		Providers: []config.DashboardProviderConfig{
			{AccountID: "openrouter", Enabled: true},
			{AccountID: "codex-cli", Enabled: false},
			{AccountID: "cursor-ide", Enabled: false},
		},
	})

	if len(disabled) != 2 {
		t.Fatalf("disabled len = %d, want 2", len(disabled))
	}
	if !disabled["codex-cli"] {
		t.Fatal("expected codex-cli to be disabled")
	}
	if !disabled["cursor-ide"] {
		t.Fatal("expected cursor-ide to be disabled")
	}
	if disabled["openrouter"] {
		t.Fatal("expected openrouter to be enabled")
	}
}

func TestReadModelTemplatesFromRequest_ExcludesDisabledAccounts(t *testing.T) {
	templates := readModelTemplatesFromRequest(daemonReadModelRequest{
		Accounts: []daemonReadModelAccount{
			{AccountID: "openrouter", ProviderID: "openrouter"},
			{AccountID: "codex-cli", ProviderID: "codex"},
		},
	}, map[string]bool{"codex-cli": true})

	if len(templates) != 1 {
		t.Fatalf("templates len = %d, want 1", len(templates))
	}
	if _, ok := templates["codex-cli"]; ok {
		t.Fatal("did not expect codex-cli template")
	}
	if got, ok := templates["openrouter"]; !ok || got.ProviderID != "openrouter" {
		t.Fatalf("openrouter template missing or invalid: %+v", got)
	}
}
