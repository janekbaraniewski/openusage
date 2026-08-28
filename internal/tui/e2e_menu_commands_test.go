package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func createTestModelWithData() Model {
	accounts := []core.AccountConfig{
		{ID: "antigravity", Provider: "antigravity"},
		{ID: "opencode-mohammed", Provider: "opencode"},
		{ID: "command_code", Provider: "command_code"},
	}
	m := NewModel(0.2, 0.05, true, config.DashboardConfig{}, accounts, core.TimeWindow30d)
	m.width = 100
	m.height = 35

	fifty := 50.0
	zero := 0.0
	hundred := 100.0
	now := time.Now()

	snaps := map[string]core.UsageSnapshot{
		"antigravity": {
			AccountID:  "antigravity",
			ProviderID: "antigravity",
			Status:     core.StatusOK,
			Timestamp:  now,
			Metrics: map[string]core.Metric{
				"quota_gemini_weekly": {Remaining: &hundred},
				"quota_gemini_5h":     {Remaining: &fifty},
			},
			Resets: map[string]time.Time{
				"quota_gemini_weekly": now.Add(24 * time.Hour),
				"quota_gemini_5h":     now.Add(2 * time.Hour),
			},
		},
		"opencode-mohammed": {
			AccountID:  "opencode-mohammed",
			ProviderID: "opencode",
			Status:     core.StatusOK,
			Timestamp:  now,
			Metrics: map[string]core.Metric{
				"rolling_usage": {Remaining: &fifty},
			},
			Resets: map[string]time.Time{
				"rolling_usage": now.Add(4 * time.Hour),
			},
		},
		"command_code": {
			AccountID:  "command_code",
			ProviderID: "command_code",
			Status:     core.StatusLimited,
			Timestamp:  now,
			Metrics: map[string]core.Metric{
				"five_hour_usage": {Remaining: &hundred},
				"weekly_usage":    {Remaining: &zero},
			},
			Resets: map[string]time.Time{
				"weekly_usage": now.Add(48 * time.Hour),
			},
		},
	}

	updated, _ := m.Update(SnapshotsMsg{
		Snapshots:  snaps,
		TimeWindow: core.TimeWindow30d,
	})
	return updated.(Model)
}

func TestE2E_MenuAndKeyNavigationWithoutBugs(t *testing.T) {
	m := createTestModelWithData()

	// 1. Assert initial dashboard renders properly with Antigravity headers and auto-refresh
	view := m.View()
	if !strings.Contains(view, "◈ GEMINI MODELS") && !strings.Contains(view, "antigravity") {
		t.Errorf("expected diamond bullet GEMINI MODELS in view")
	}
	if !strings.Contains(view, "auto-refresh ⟳ 10s") {
		t.Errorf("expected auto-refresh hint in footer")
	}

	// Test rendering each card as cursor moves
	ids := m.filteredIDs()
	for i, id := range ids {
		m.cursor = i
		v := m.View()
		if v == "" {
			t.Fatalf("empty view for tile %s", id)
		}
	}

	// 2. Test Help Overlay Toggle ('?')
	mHelp, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !mHelp.(Model).showHelp {
		t.Fatal("expected showHelp=true after pressing '?'")
	}
	helpView := mHelp.(Model).View()
	if !strings.Contains(helpView, "Themes") && !strings.Contains(helpView, "dashboard") {
		t.Errorf("expected Help text in help overlay view, got: %s", helpView)
	}
	// Any key dismisses help
	mDismissHelp, _ := mHelp.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mDismissHelp.(Model).showHelp {
		t.Fatal("expected showHelp=false after pressing Esc")
	}

	// 3. Test Navigation Keys on Dashboard (j, k, up, down, pgup, pgdn, home, end)
	allNavKeys := []tea.KeyMsg{
		{Type: tea.KeyDown},
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyUp},
		{Type: tea.KeyRunes, Runes: []rune("k")},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyHome},
		{Type: tea.KeyEnd},
		{Type: tea.KeyRunes, Runes: []rune("w")}, // cycle time window
		{Type: tea.KeyRunes, Runes: []rune("c")}, // toggle cost override
		{Type: tea.KeyRunes, Runes: []rune("r")}, // refresh
	}
	curr := mDismissHelp.(Model)
	for _, k := range allNavKeys {
		next, _ := curr.Update(k)
		curr = next.(Model)
		v := curr.View()
		if v == "" {
			t.Fatalf("empty view after pressing key %v", k)
		}
	}

	// 4. Test Filter Mode ('/')
	mFilter, _ := curr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !mFilter.(Model).filter.active {
		t.Fatal("expected filter.active=true after pressing '/'")
	}
	// Type filter query "gemini"
	mType, _ := mFilter.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	mType, _ = mType.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	mType, _ = mType.(Model).Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	filterView := mType.(Model).View()
	if filterView == "" {
		t.Fatal("empty view while filter is active")
	}
	// Exit filter with Esc
	mExitFilter, _ := mType.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mExitFilter.(Model).filter.text != "" {
		// First esc clears text
		mExitFilter, _ = mExitFilter.(Model).Update(tea.KeyMsg{Type: tea.KeyEsc})
	}
	curr = mExitFilter.(Model)

	// 5. Test Settings Modal ('p') and Tab Navigation across all tabs
	mSettings, _ := curr.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("p")})
	if !mSettings.(Model).settings.show {
		t.Fatal("expected settings.show=true after pressing 'p'")
	}
	sModel := mSettings.(Model)

	// Navigate through all settings tabs with Tab / Shift+Tab / 1-7
	for tabIdx := 0; tabIdx < int(settingsTabCount); tabIdx++ {
		sView := sModel.View()
		if sView == "" {
			t.Fatalf("empty view in settings tab %d", sModel.settings.tab)
		}
		// Press Tab to go to next tab
		nextTab, _ := sModel.Update(tea.KeyMsg{Type: tea.KeyTab})
		sModel = nextTab.(Model)
	}

	// Scroll inside settings tab with j/k and mouse wheel
	sModelUpdated, _ := sModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
	sModel = sModelUpdated.(Model)
	sModelWheel, _ := sModel.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonWheelDown,
	})
	sModel = sModelWheel.(Model)

	// Close settings modal with Esc
	mCloseSettings, _ := sModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mCloseSettings.(Model).settings.show {
		t.Fatal("expected settings.show=false after pressing Esc in settings")
	}
	curr = mCloseSettings.(Model)

	// 6. Test Detail Mode entry, section switching, and exit
	mDetail, _ := curr.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if mDetail.(Model).mode != modeDetail {
		t.Fatal("expected modeDetail after pressing Enter")
	}
	dModel := mDetail.(Model)
	dView := dModel.View()
	if dView == "" {
		t.Fatal("empty view in detail mode")
	}

	// Detail mode navigation (Tab, Shift+Tab, j, k, [, ], PgUp, PgDn, g, G)
	detailNavKeys := []tea.KeyMsg{
		{Type: tea.KeyTab},
		{Type: tea.KeyShiftTab},
		{Type: tea.KeyRight},
		{Type: tea.KeyLeft},
		{Type: tea.KeyDown},
		{Type: tea.KeyUp},
		{Type: tea.KeyRunes, Runes: []rune("j")},
		{Type: tea.KeyRunes, Runes: []rune("k")},
		{Type: tea.KeyRunes, Runes: []rune("[")},
		{Type: tea.KeyRunes, Runes: []rune("]")},
		{Type: tea.KeyPgDown},
		{Type: tea.KeyPgUp},
		{Type: tea.KeyRunes, Runes: []rune("g")},
		{Type: tea.KeyRunes, Runes: []rune("G")},
		{Type: tea.KeyRunes, Runes: []rune("r")},
	}
	for _, k := range detailNavKeys {
		next, _ := dModel.Update(k)
		dModel = next.(Model)
		v := dModel.View()
		if v == "" {
			t.Fatalf("empty view in detail mode after key %v", k)
		}
	}

	// Exit detail mode with Esc
	mExitDetail, _ := dModel.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if mExitDetail.(Model).mode != modeList {
		t.Fatal("expected modeList after pressing Esc in detail mode")
	}
}
