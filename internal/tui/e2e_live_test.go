package tui

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/antigravity"
	"github.com/janekbaraniewski/openusage/internal/providers/command_code"
	"github.com/janekbaraniewski/openusage/internal/providers/opencode"
)

func TestE2E_LiveProvidersRender(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	ag := antigravity.New()
	oc := opencode.New()
	cc := command_code.New()

	snaps := make(map[string]core.UsageSnapshot)
	accounts := []core.AccountConfig{
		{ID: "antigravity", Provider: "antigravity"},
		{ID: "opencode-mohammed", Provider: "opencode"},
		{ID: "command_code", Provider: "command_code", APIKeyEnv: "COMMAND_CODE_API_KEY"},
	}

	// 1. Fetch live snapshots
	snapAg, errAg := ag.Fetch(ctx, accounts[0])
	if errAg == nil && snapAg.Status != "" {
		snaps[accounts[0].ID] = snapAg
	}

	snapOc, errOc := oc.Fetch(ctx, accounts[1])
	if errOc == nil && snapOc.Status != "" {
		snaps[accounts[1].ID] = snapOc
	}

	snapCc, errCc := cc.Fetch(ctx, accounts[2])
	if errCc == nil && snapCc.Status != "" {
		snaps[accounts[2].ID] = snapCc
	}

	if len(snaps) == 0 {
		t.Skip("No live provider credentials available in this environment")
	}

	// 2. Initialize TUI model with live snapshots
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)
	m.width = 120
	m.height = 40

	updated, _ := m.Update(SnapshotsMsg{
		Snapshots:  snaps,
		TimeWindow: core.TimeWindow30d,
	})
	model := updated.(Model)

	// 3. Render View for each provider by navigating cursor
	ids := model.filteredIDs()
	for i, id := range ids {
		model.cursor = i
		view := model.View()
		if view == "" {
			t.Fatalf("rendered view is empty for provider %s", id)
		}

		snap := snaps[id]
		switch snap.ProviderID {
		case "antigravity":
			if !strings.Contains(view, "GEMINI MODELS") && !strings.Contains(view, "antigravity") {
				t.Errorf("expected Antigravity content in view, got:\n%s", view)
			}
			if !strings.Contains(view, "Resets in") && !strings.Contains(view, "remaining") {
				t.Errorf("expected reset countdown or remaining line for Antigravity, got:\n%s", view)
			}
		case "opencode":
			if !strings.Contains(view, "OPENCODE") && !strings.Contains(view, "opencode") {
				t.Errorf("expected OpenCode content in view, got:\n%s", view)
			}
		case "command_code":
			if !strings.Contains(view, "COMMAND CODE") && !strings.Contains(view, "command_code") {
				t.Errorf("expected Command Code content in view, got:\n%s", view)
			}
		}
	}

	// 4. Test Auto-Refresh trigger
	refreshCalled := false
	model.SetOnRefresh(func(window core.TimeWindow) {
		refreshCalled = true
	})
	refreshedModel, cmd := model.handleAutoRefresh()
	if !refreshedModel.(Model).refreshing && !refreshCalled {
		t.Error("expected handleAutoRefresh to trigger refresh")
	}
	if cmd == nil {
		t.Error("expected autoRefreshCmd to be returned for next tick")
	}

	// 5. Test Mouse Click selection and detail view entry
	clickedModel, _ := model.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      20,
		Y:      5,
	})
	mAfterClick := clickedModel.(Model)
	if mAfterClick.cursor < 0 || mAfterClick.cursor >= len(model.filteredIDs()) {
		t.Errorf("cursor after click out of bounds: %d", mAfterClick.cursor)
	}

	// Click same tile again to enter detail mode
	detailModel, _ := mAfterClick.Update(tea.MouseMsg{
		Action: tea.MouseActionPress,
		Button: tea.MouseButtonLeft,
		X:      20,
		Y:      5,
	})
	mDetail := detailModel.(Model)
	if mDetail.mode != modeDetail {
		t.Errorf("expected modeDetail after double-clicking tile, got %v", mDetail.mode)
	}
	detailView := mDetail.View()
	if detailView == "" {
		t.Fatal("rendered detail view is empty")
	}

	// Exit detail mode with Esc
	backModel, _ := mDetail.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if backModel.(Model).mode != modeList {
		t.Errorf("expected modeList after pressing Esc in detail mode, got %v", backModel.(Model).mode)
	}
}
