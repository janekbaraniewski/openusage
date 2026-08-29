package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestWebProjectorDemoSnapshot(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	snap := core.NewUsageSnapshot("claude_code", "claude-code")
	snap.Timestamp = now
	snap.Status = core.StatusOK
	snap.Message = "~$42.18 today"
	used := 38.0
	limit := 100.0
	rem := 62.0
	snap.Metrics = map[string]core.Metric{
		"today_api_cost":  {Used: &used, Unit: "USD", Window: "today"},
		"usage_five_hour": {Used: &used, Limit: &limit, Remaining: &rem, Unit: "%", Window: "rolling-5h"},
	}

	p := WebProjector{
		TimeWindow:    core.TimeWindow3d,
		WarnThreshold: 0.25,
		CritThreshold: 0.1,
		UsageMode:     config.UsageModeRemaining,
		TileWidth:     72,
		DetailWidth:   80,
		Now:           now,
	}
	view := p.ProjectSnapshot(snap, "Claude Code")

	if view.Key != "claude_code:claude-code" {
		t.Fatalf("key = %q", view.Key)
	}
	if view.Status != string(core.StatusOK) {
		t.Fatalf("status = %q", view.Status)
	}
	if view.Summary == "" {
		t.Fatal("expected summary")
	}
	if len(view.TileLines) < 3 {
		t.Fatalf("expected tile lines, got %d", len(view.TileLines))
	}
	if view.GaugePercent < 0 {
		t.Fatalf("expected gauge percent, got %v", view.GaugePercent)
	}
	body := strings.Join(view.TileLines, "\n")
	if !strings.Contains(body, "claude-code") {
		t.Fatalf("tile should include account id:\n%s", body)
	}
}

func TestStripANSI(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m world"
	if got := StripANSI(in); got != "hello world" {
		t.Fatalf("StripANSI = %q", got)
	}
}

func TestOrderSnapshotsRespectsDashboardOrder(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	snaps := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Timestamp:  now,
			Status:     core.StatusOK,
		},
		"claude-code": {
			ProviderID: "claude_code",
			AccountID:  "claude-code",
			Timestamp:  now,
			Status:     core.StatusOK,
		},
	}
	cfg := config.DefaultConfig()
	cfg.Dashboard.Providers = []config.DashboardProviderConfig{
		{AccountID: "claude-code", Enabled: true},
		{AccountID: "cursor-ide", Enabled: true},
	}
	cfg.Accounts = []core.AccountConfig{
		{ID: "claude-code", Provider: "claude_code"},
		{ID: "cursor-ide", Provider: "cursor"},
	}
	p := NewWebProjectorFromConfig(cfg)
	ordered := p.OrderSnapshots(snaps)
	if len(ordered) != 2 {
		t.Fatalf("ordered len = %d", len(ordered))
	}
	if ordered[0].AccountID != "claude-code" {
		t.Fatalf("first = %q, want claude-code", ordered[0].AccountID)
	}

	cfg.Dashboard.Providers[1].Enabled = false
	p = NewWebProjectorFromConfig(cfg)
	ordered = p.OrderSnapshots(snaps)
	if len(ordered) != 1 || ordered[0].AccountID != "claude-code" {
		t.Fatalf("disabled account should be excluded, got %+v", ordered)
	}
}

func TestThemeTokensForName(t *testing.T) {
	tokens := ThemeTokensForName("Tokyo Night")
	if tokens.Base == "" || tokens.Text == "" {
		t.Fatalf("incomplete theme tokens: %+v", tokens)
	}
	if tokens.Name == "" {
		t.Fatal("expected theme name")
	}
}
