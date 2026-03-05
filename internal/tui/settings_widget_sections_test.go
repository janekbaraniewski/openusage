package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestHandleSettingsModalKey_WidgetSectionsToggle(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := Model{
		showSettingsModal: true,
		settingsModalTab:  settingsTabWidgetSections,
		widgetSections: []config.DashboardWidgetSection{
			{ID: core.DashboardSectionTopUsageProgress, Enabled: true},
			{ID: core.DashboardSectionOtherData, Enabled: true},
		},
	}

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected persist command when toggling widget section")
	}
	if got.widgetSections[0].Enabled {
		t.Fatal("expected first widget section to be toggled off")
	}
}

func TestHandleSettingsModalKey_WidgetSectionsMoveRow(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := Model{
		showSettingsModal: true,
		settingsModalTab:  settingsTabWidgetSections,
		widgetSections: []config.DashboardWidgetSection{
			{ID: core.DashboardSectionTopUsageProgress, Enabled: true},
			{ID: core.DashboardSectionOtherData, Enabled: true},
		},
	}

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected persist command when moving widget section")
	}
	if got.settingsSectionRowCursor != 1 {
		t.Fatalf("settingsSectionRowCursor = %d, want 1", got.settingsSectionRowCursor)
	}
	if got.widgetSections[0].ID != core.DashboardSectionOtherData {
		t.Fatalf("first section ID = %q, want %q", got.widgetSections[0].ID, core.DashboardSectionOtherData)
	}
}

func TestHandleSettingsModalKey_WidgetSectionsReorderAffectsRenderedWidget(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{
			{ID: "openai", Provider: "openai"},
		},
		core.TimeWindow7d,
	)
	m.showSettingsModal = true
	m.settingsModalTab = settingsTabWidgetSections

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	got := updated.(Model)
	if cmd == nil {
		t.Fatal("expected persist command when reordering widget sections")
	}
	if got.settingsSectionRowCursor != 1 {
		t.Fatalf("settingsSectionRowCursor = %d, want 1", got.settingsSectionRowCursor)
	}

	order := dashboardWidget("openai").EffectiveStandardSectionOrder()
	if len(order) < 2 {
		t.Fatalf("effective section order too short: %#v", order)
	}
	if order[0] != core.DashboardSectionModelBurn || order[1] != core.DashboardSectionTopUsageProgress {
		t.Fatalf("effective section order prefix = %#v, want [model_burn top_usage_progress]", order[:2])
	}
}
