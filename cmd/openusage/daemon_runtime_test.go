package main

import (
	"fmt"
	"testing"
)

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

func TestSyncStatusMessage(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "nil", err: nil, want: "Connecting to telemetry daemon..."},
		{name: "not installed", err: fmt.Errorf("telemetry daemon service is not installed"), want: "Telemetry daemon not installed. Run: openusage telemetry daemon install"},
		{name: "upgrade", err: fmt.Errorf("telemetry daemon is out of date"), want: "Upgrading telemetry daemon..."},
		{name: "unavailable", err: fmt.Errorf("telemetry daemon unavailable"), want: "Waiting for telemetry daemon..."},
		{name: "generic", err: fmt.Errorf("something else"), want: "Connecting to telemetry daemon..."},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if got := syncStatusMessage(tt.err); got != tt.want {
				t.Fatalf("syncStatusMessage(%v) = %q, want %q", tt.err, got, tt.want)
			}
		})
	}
}

func TestSnapshotsHaveUsableData(t *testing.T) {
	if snapshotsHaveUsableData(nil) {
		t.Fatal("snapshotsHaveUsableData(nil) = true, want false")
	}
	notReady := map[string]core.UsageSnapshot{
		"a": {
			Status:  core.StatusUnknown,
			Message: "Connecting to telemetry daemon...",
		},
	}
	if snapshotsHaveUsableData(notReady) {
		t.Fatal("snapshotsHaveUsableData(notReady) = true, want false")
	}
	ready := map[string]core.UsageSnapshot{
		"a": {
			Status: core.StatusUnknown,
			Metrics: map[string]core.Metric{
				"usage_daily": {Used: float64Ptr(1), Unit: "USD"},
			},
		},
	}
	if !snapshotsHaveUsableData(ready) {
		t.Fatal("snapshotsHaveUsableData(ready) = false, want true")
	}
}

func float64Ptr(v float64) *float64 { return &v }
