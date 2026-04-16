package tui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

func TestHandleSettingsModalKey_WidgetSectionsToggle(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := Model{
		settings: settingsState{show: true, tab: settingsTabWidgetSections},
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

func TestWidgetSectionEntries_AppendsNewDefaultSectionsForLegacyConfig(t *testing.T) {
	m := Model{
		widgetSections: []config.DashboardWidgetSection{
			{ID: core.DashboardSectionTopUsageProgress, Enabled: true},
			{ID: core.DashboardSectionOtherData, Enabled: true},
		},
	}

	entries := m.widgetSectionEntries()
	foundProjectBreakdown := false
	projectBreakdownCount := 0
	for _, entry := range entries {
		if entry.ID == core.DashboardSectionProjectBreakdown {
			foundProjectBreakdown = true
			projectBreakdownCount++
		}
	}
	if !foundProjectBreakdown {
		t.Fatalf("expected %q to be present in resolved widget sections, got %#v", core.DashboardSectionProjectBreakdown, entries)
	}
	if projectBreakdownCount != 1 {
		t.Fatalf("expected exactly one %q entry, got %d", core.DashboardSectionProjectBreakdown, projectBreakdownCount)
	}
}

func TestHandleSettingsModalKey_WidgetSectionsMoveRow(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := Model{
		settings: settingsState{show: true, tab: settingsTabWidgetSections},
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
	if got.settings.sectionRowCursor != 1 {
		t.Fatalf("settingsSectionRowCursor = %d, want 1", got.settings.sectionRowCursor)
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
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("J")})
	got := updated.(Model)
	if cmd == nil {
		t.Fatal("expected persist command when reordering widget sections")
	}
	if got.settings.sectionRowCursor != 1 {
		t.Fatalf("settingsSectionRowCursor = %d, want 1", got.settings.sectionRowCursor)
	}

	order := dashboardWidget("openai").EffectiveStandardSectionOrder()
	if len(order) < 2 {
		t.Fatalf("effective section order too short: %#v", order)
	}
	if order[0] != core.DashboardSectionModelBurn || order[1] != core.DashboardSectionTopUsageProgress {
		t.Fatalf("effective section order prefix = %#v, want [model_burn top_usage_progress]", order[:2])
	}
}

func TestRenderSettingsWidgetSectionsBody_RendersListOnly(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections

	body := stripANSI(m.renderSettingsWidgetSectionsBody(96, 20))
	if !strings.Contains(body, "Global Widget Sections") {
		t.Fatalf("expected widget sections list in body, got: %q", body)
	}
	if !strings.Contains(body, "Hide sections with no data:") {
		t.Fatalf("expected hide-empty toggle in body, got: %q", body)
	}
	if strings.Contains(body, "Live Preview") {
		t.Fatalf("expected preview to render outside body panel, got: %q", body)
	}
}

func TestRenderSettingsModalOverlay_WidgetSectionsIncludesSeparatePreviewPanel(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections
	m.width = 180
	m.height = 50

	overlay := stripANSI(m.renderSettingsModalOverlay())
	if !strings.Contains(overlay, "Settings") {
		t.Fatalf("expected settings panel in overlay, got: %q", overlay)
	}
	if !strings.Contains(overlay, "Widget Preview") {
		t.Fatalf("expected separate widget preview panel in overlay, got: %q", overlay)
	}
	if !strings.Contains(overlay, "Live Preview") {
		t.Fatalf("expected live preview content in preview panel, got: %q", overlay)
	}
}

func TestHandleSettingsModalKey_WidgetSectionsPreviewScroll(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.settings.show = true
	m.settings.tab = settingsTabWidgetSections
	m.settings.previewOffset = 0

	updatedDown, _ := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyPgDown})
	gotDown := updatedDown.(Model)
	if gotDown.settings.previewOffset <= 0 {
		t.Fatalf("expected preview offset to increase on PgDown, got %d", gotDown.settings.previewOffset)
	}

	updatedUp, _ := gotDown.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyPgUp})
	gotUp := updatedUp.(Model)
	if gotUp.settings.previewOffset >= gotDown.settings.previewOffset {
		t.Fatalf("expected preview offset to decrease on PgUp, got before=%d after=%d", gotDown.settings.previewOffset, gotUp.settings.previewOffset)
	}
}

func TestHandleSettingsModalKey_WidgetSectionsToggleHideEmptySections(t *testing.T) {
	m := Model{
		settings:               settingsState{show: true, tab: settingsTabWidgetSections},
		hideSectionsWithNoData: false,
	}

	updated, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("h")})
	got := updated.(Model)

	if cmd == nil {
		t.Fatal("expected persist command when toggling hide-empty setting")
	}
	if !got.hideSectionsWithNoData {
		t.Fatal("expected hideSectionsWithNoData to be true after pressing h")
	}
}

func TestRenderSettingsModalTabs_AlwaysSingleRow(t *testing.T) {
	m := Model{
		settings: settingsState{show: true, tab: settingsTabWidgetSections},
	}

	tabs := stripANSI(m.renderSettingsModalTabs(72))
	if strings.Contains(tabs, "\n") {
		t.Fatalf("expected tabs in a single row, got: %q", tabs)
	}
	for i := 1; i <= int(settingsTabCount); i++ {
		if !strings.Contains(tabs, fmt.Sprintf("%d ", i)) {
			t.Fatalf("expected tab index %d to be present, got: %q", i, tabs)
		}
	}
}

func TestRenderSettingsWidgetSectionsPreview_ReflectsSectionVisibility(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)

	defaultPreview := stripANSI(m.renderSettingsWidgetSectionsPreview(60, 16))
	if !strings.Contains(defaultPreview, "Usage 5h") {
		t.Fatalf("expected top usage section in default preview, got: %q", defaultPreview)
	}

	m.setWidgetSectionEntries([]config.DashboardWidgetSection{
		{ID: core.DashboardSectionTopUsageProgress, Enabled: false},
		{ID: core.DashboardSectionModelBurn, Enabled: true},
		{ID: core.DashboardSectionOtherData, Enabled: true},
	})
	updatedPreview := stripANSI(m.renderSettingsWidgetSectionsPreview(60, 16))
	if strings.Contains(updatedPreview, "Usage 5h") {
		t.Fatalf("expected top usage section to be hidden after disabling it, got: %q", updatedPreview)
	}
	if !strings.Contains(updatedPreview, "Model Burn") {
		t.Fatalf("expected a different section to appear in preview after toggle, got: %q", updatedPreview)
	}
}

func TestSettingsWidgetPreviewBodyHeight_SideBySideShrinksToContent(t *testing.T) {
	t.Cleanup(func() { setDashboardWidgetSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow7d,
	)
	m.width = 220
	m.height = 80
	m.setWidgetSectionEntries([]config.DashboardWidgetSection{
		{ID: core.DashboardSectionTopUsageProgress, Enabled: true},
		{ID: core.DashboardSectionModelBurn, Enabled: true},
		{ID: core.DashboardSectionToolUsage, Enabled: false},
		{ID: core.DashboardSectionClientBurn, Enabled: false},
		{ID: core.DashboardSectionLanguageBurn, Enabled: false},
		{ID: core.DashboardSectionCodeStats, Enabled: false},
		{ID: core.DashboardSectionDailyUsage, Enabled: false},
		{ID: core.DashboardSectionProviderBurn, Enabled: false},
		{ID: core.DashboardSectionUpstreamProviders, Enabled: false},
		{ID: core.DashboardSectionMCPUsage, Enabled: false},
		{ID: core.DashboardSectionOtherData, Enabled: false},
	})

	got := m.settingsWidgetPreviewBodyHeight(92, 20, true)
	maxViewport := (m.height - 12) + 1
	if got >= maxViewport {
		t.Fatalf("expected side-by-side preview height to shrink below viewport max for short content, got=%d max=%d", got, maxViewport)
	}
}

func TestRenderSettingsDetailSectionsPreview_ShowsTrendCharts(t *testing.T) {
	t.Cleanup(func() { setDetailSectionOverrides(nil) })

	m := NewModel(
		0.2,
		0.05,
		false,
		config.DashboardConfig{},
		[]core.AccountConfig{{ID: "claude-preview", Provider: "claude_code"}},
		core.TimeWindow30d,
	)
	m.setDetailWidgetSectionEntries([]config.DetailWidgetSection{
		{ID: core.DetailSectionTrends, Enabled: true},
	})

	preview := stripANSI(m.renderSettingsDetailSectionsPreview(110, 32))
	if !strings.Contains(preview, "Daily Usage") {
		t.Fatalf("expected daily trend summary in detail preview, got: %q", preview)
	}
	if !strings.Contains(preview, "Requests") || !strings.Contains(preview, "Analytics Cost") {
		t.Fatalf("expected trend charts in detail preview, got: %q", preview)
	}
}
