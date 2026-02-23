package main

import "testing"

import "github.com/janekbaraniewski/openusage/internal/core"

func TestDaemonReadModelRequestFromAccounts_DedupsAndNormalizes(t *testing.T) {
	req := daemonReadModelRequestFromAccounts(
		[]core.AccountConfig{
			{ID: " codex-cli ", Provider: " codex "},
			{ID: "codex-cli", Provider: "openai"},
			{ID: "openrouter", Provider: "openrouter"},
			{ID: "", Provider: "openrouter"},
		},
		map[string]string{
			" Anthropic ": " Claude_Code ",
			"":            "openrouter",
			"openai":      "",
		},
	)

	if len(req.Accounts) != 2 {
		t.Fatalf("accounts len = %d, want 2", len(req.Accounts))
	}
	if req.Accounts[0].AccountID != "codex-cli" || req.Accounts[0].ProviderID != "codex" {
		t.Fatalf("first account = %+v, want codex-cli/codex", req.Accounts[0])
	}
	if req.Accounts[1].AccountID != "openrouter" || req.Accounts[1].ProviderID != "openrouter" {
		t.Fatalf("second account = %+v, want openrouter/openrouter", req.Accounts[1])
	}
	if len(req.ProviderLinks) != 1 {
		t.Fatalf("provider links len = %d, want 1", len(req.ProviderLinks))
	}
	if got := req.ProviderLinks["anthropic"]; got != "claude_code" {
		t.Fatalf("provider link anthropic = %q, want claude_code", got)
	}
}

func TestReadModelTemplatesFromRequest_SeedsAccounts(t *testing.T) {
	templates := readModelTemplatesFromRequest(daemonReadModelRequest{
		Accounts: []daemonReadModelAccount{
			{AccountID: "openrouter", ProviderID: "openrouter"},
			{AccountID: "openrouter", ProviderID: "openrouter"},
			{AccountID: "cursor-ide", ProviderID: "cursor"},
		},
	}, nil)

	if len(templates) != 2 {
		t.Fatalf("templates len = %d, want 2", len(templates))
	}
	if got := templates["openrouter"]; got.Status != core.StatusUnknown || got.Message != "" {
		t.Fatalf("openrouter template = %+v, want UNKNOWN with empty message", got)
	}
	if got := templates["cursor-ide"]; got.ProviderID != "cursor" || got.AccountID != "cursor-ide" {
		t.Fatalf("cursor template = %+v, want cursor/cursor-ide", got)
	}
}
