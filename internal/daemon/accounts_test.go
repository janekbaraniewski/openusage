package daemon

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFilterAccountsByDashboard_DefaultEnabled(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "openrouter", Provider: "openrouter"},
		{ID: "codex-cli", Provider: "codex"},
	}

	filtered := FilterAccountsByDashboard(accounts, config.DashboardConfig{})
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}
}

func TestFilterAccountsByDashboard_ExcludesDisabled(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "openrouter", Provider: "openrouter"},
		{ID: "codex-cli", Provider: "codex"},
		{ID: "claude-code", Provider: "claude_code"},
	}

	filtered := FilterAccountsByDashboard(accounts, config.DashboardConfig{
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
	disabled := DisabledAccountsFromDashboard(config.DashboardConfig{
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
	templates := ReadModelTemplatesFromRequest(ReadModelRequest{
		Accounts: []ReadModelAccount{
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

func TestBuildReadModelRequest_DedupsAndNormalizes(t *testing.T) {
	req := BuildReadModelRequest(
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
	templates := ReadModelTemplatesFromRequest(ReadModelRequest{
		Accounts: []ReadModelAccount{
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

func TestSnapshotsHaveUsableData(t *testing.T) {
	if SnapshotsHaveUsableData(nil) {
		t.Fatal("SnapshotsHaveUsableData(nil) = true, want false")
	}
	notReady := map[string]core.UsageSnapshot{
		"a": {
			Status:  core.StatusUnknown,
			Message: "Connecting to telemetry daemon...",
		},
	}
	if SnapshotsHaveUsableData(notReady) {
		t.Fatal("SnapshotsHaveUsableData(notReady) = true, want false")
	}
	ready := map[string]core.UsageSnapshot{
		"a": {
			Status: core.StatusUnknown,
			Metrics: map[string]core.Metric{
				"usage_daily": {Used: float64Ptr(1), Unit: "USD"},
			},
		},
	}
	if !SnapshotsHaveUsableData(ready) {
		t.Fatal("SnapshotsHaveUsableData(ready) = false, want true")
	}
}

func float64Ptr(v float64) *float64 { return &v }
