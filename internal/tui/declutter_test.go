package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestTileDeclutter_NoRedundantUsageSubheader(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "opencode-test", Provider: "opencode"},
		{ID: "openrouter-test", Provider: "openrouter"},
	}
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	snapOc := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode-test",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: func(f float64) *float64 { return &f }(80)},
		},
	}
	snapOr := core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter-test",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credits_remaining": {Remaining: func(f float64) *float64 { return &f }(50)},
		},
	}

	tileOc := m.renderTile(snapOc, false, false, 80, 15, 0)
	tileOr := m.renderTile(snapOr, false, false, 80, 15, 0)

	// Ensure redundant subheader line (e.g. "Usage · opencode" / "Credits · openrouter") is not rendered.
	if strings.Contains(tileOc, "Usage · opencode") || strings.Contains(tileOc, "⚡ Usage") {
		t.Errorf("expected tile header not to contain redundant usage subheader, got:\n%s", tileOc)
	}
	if strings.Contains(tileOr, "Credits · openrouter") || strings.Contains(tileOr, "💰 Credits") {
		t.Errorf("expected tile header not to contain redundant credits subheader, got:\n%s", tileOr)
	}

	// Ensure time window pill (e.g. "⏱ 30d") is not rendered on tile header.
	if strings.Contains(tileOc, "⏱ 30d") || strings.Contains(tileOr, "⏱ 30d") {
		t.Errorf("expected tile header not to contain time window pill")
	}
}

func TestTileDeclutter_NoVerboseModelsTierDescription(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "antigravity-test", Provider: "antigravity"},
		{ID: "opencode-test", Provider: "opencode"},
		{ID: "command-code-test", Provider: "command_code"},
	}
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	// 1. Antigravity
	snapAg := core.UsageSnapshot{
		ProviderID: "antigravity",
		AccountID:  "antigravity-test",
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"quota_gemini_weekly": {Remaining: func(f float64) *float64 { return &f }(90)},
		},
	}
	tileAg := m.renderTile(snapAg, false, false, 80, 15, 0)
	if strings.Contains(tileAg, "Models within this group") {
		t.Errorf("expected Antigravity tile not to contain 'Models within this group', got:\n%s", tileAg)
	}

	// 2. OpenCode
	snapOc := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode-test",
		Timestamp:  now,
		Attributes: map[string]string{
			"available_models_count": "64",
		},
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: func(f float64) *float64 { return &f }(80)},
		},
	}
	tileOc := m.renderTile(snapOc, false, false, 80, 15, 0)
	if strings.Contains(tileOc, "Models within this tier") {
		t.Errorf("expected OpenCode tile not to contain 'Models within this tier', got:\n%s", tileOc)
	}

	// 3. Command Code
	snapCc := core.UsageSnapshot{
		ProviderID: "command_code",
		AccountID:  "command-code-test",
		Timestamp:  now,
		Attributes: map[string]string{
			"weekly_cap": "1000",
		},
		Metrics: map[string]core.Metric{
			"weekly_usage": {Remaining: func(f float64) *float64 { return &f }(50)},
		},
	}
	tileCc := m.renderTile(snapCc, false, false, 80, 15, 0)
	if strings.Contains(tileCc, "Unlimited Turns") {
		t.Errorf("expected Command Code tile not to contain 'Unlimited Turns', got:\n%s", tileCc)
	}
}

func TestDetailDeclutter_NoVerboseModelsTierDescription(t *testing.T) {
	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)

	// Antigravity detail
	snapAg := core.UsageSnapshot{
		ProviderID: "antigravity",
		AccountID:  "antigravity-test",
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"quota_gemini_weekly": {Remaining: func(f float64) *float64 { return &f }(90)},
		},
	}
	detailAg := RenderDetailContent(snapAg, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if strings.Contains(detailAg, "Models within this group") {
		t.Errorf("expected Antigravity detail not to contain 'Models within this group', got:\n%s", detailAg)
	}

	// OpenCode detail
	snapOc := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode-test",
		Timestamp:  now,
		Attributes: map[string]string{
			"available_models_count": "64",
		},
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: func(f float64) *float64 { return &f }(80)},
		},
	}
	detailOc := RenderDetailContent(snapOc, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if strings.Contains(detailOc, "Models within this tier") {
		t.Errorf("expected OpenCode detail not to contain 'Models within this tier', got:\n%s", detailOc)
	}

	headerLines := strings.Join(strings.Split(detailOc, "\n")[:2], "\n")
	if strings.Contains(headerLines, "⚡ Usage") {
		t.Errorf("expected compact detail header not to contain '⚡ Usage', got:\n%s", headerLines)
	}
}

func TestListDeclutter_NoDuplicateTagBadge(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "opencode-test", Provider: "opencode"},
	}
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)

	snap := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode-test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: func(f float64) *float64 { return &f }(80)},
		},
	}

	item := m.renderListItem(snap, false, 80)
	if strings.Contains(item, "⚡ Usage") || strings.Contains(item, "Usage OK") {
		t.Errorf("expected list item not to have redundant '⚡ Usage' tag before badge, got:\n%s", item)
	}
}

func TestOpenCode_MonthlyUsageResetTimer(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "opencode-test", Provider: "opencode"},
	}
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)

	now := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	snap := core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode-test",
		Timestamp:  now,
		Status:     core.StatusLimited,
		Metrics: map[string]core.Metric{
			"monthly_usage_pct": {Remaining: func(f float64) *float64 { return &f }(0)},
		},
		Resets: map[string]time.Time{
			"monthly_usage": now.Add(10 * 24 * time.Hour),
		},
	}

	tile := m.renderTile(snap, false, false, 80, 15, 0)
	if !strings.Contains(tile, "Resets in") {
		t.Errorf("expected tile to render monthly reset timer 'Resets in', got:\n%s", tile)
	}

	detail := RenderDetailContent(snap, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if !strings.Contains(detail, "Resets in") {
		t.Errorf("expected detail view to render monthly reset timer 'Resets in', got:\n%s", detail)
	}
}

// test reload
