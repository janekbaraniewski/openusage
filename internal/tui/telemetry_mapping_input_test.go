package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

type fakeServices struct {
	savedSource string
	savedTarget string
	saveErr     error
	deletedSrc  string
	deleteErr   error
}

func (f *fakeServices) SaveTheme(string) error { return nil }
func (f *fakeServices) SaveDashboardProviders([]config.DashboardProviderConfig) error {
	return nil
}
func (f *fakeServices) SaveDashboardView(string) error                                    { return nil }
func (f *fakeServices) SaveDashboardWidgetSections([]config.DashboardWidgetSection) error { return nil }
func (f *fakeServices) SaveDetailWidgetSections([]config.DetailWidgetSection) error       { return nil }
func (f *fakeServices) SaveDashboardHideSectionsWithNoData(bool) error                    { return nil }
func (f *fakeServices) SaveTimeWindow(string) error                                       { return nil }
func (f *fakeServices) SaveProviderLink(source, target string) error {
	f.savedSource = source
	f.savedTarget = target
	return f.saveErr
}
func (f *fakeServices) DeleteProviderLink(source string) error {
	f.deletedSrc = source
	return f.deleteErr
}
func (f *fakeServices) ValidateAPIKey(string, string, string) (bool, string) { return true, "" }
func (f *fakeServices) SaveCredential(string, string) error                  { return nil }
func (f *fakeServices) DeleteCredential(string) error                        { return nil }
func (f *fakeServices) InstallIntegration(integrations.ID) ([]integrations.Status, error) {
	return nil, nil
}
func (f *fakeServices) ConnectBrowserSession(string, string, string, string) (core.BrowserSessionInfo, error) {
	return core.BrowserSessionInfo{}, nil
}
func (f *fakeServices) DisconnectBrowserSession(string) error { return nil }
func (f *fakeServices) LoadBrowserSessionInfo(string) core.BrowserSessionInfo {
	return core.BrowserSessionInfo{}
}
func (f *fakeServices) OpenProviderConsole(string) error     { return nil }
func (f *fakeServices) AvailableBrowsers() ([]string, error) { return nil, nil }

func telemetryFixtureModel() Model {
	return Model{
		snapshots: map[string]core.UsageSnapshot{
			"copilot": {
				ProviderID: "copilot",
				Diagnostics: map[string]string{
					"telemetry_unmapped_providers": "github-copilot,openai",
					"telemetry_unmapped_meta":      "github-copilot=unconfigured:copilot,openai=unconfigured",
				},
			},
		},
		accountProviders: map[string]string{
			"copilot": "copilot",
			"cursor":  "cursor",
		},
		settings: settingsState{
			tab:    settingsTabTelemetry,
			cursor: len(core.ValidTimeWindows), // first unmapped row
		},
	}
}

func keyOf(s string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestTelemetryRow_DownArrowAdvancesPastTimeWindowsIntoUnmappedSection(t *testing.T) {
	m := telemetryFixtureModel()
	m.settings.cursor = 0 // start on first time window

	for i := 0; i < len(core.ValidTimeWindows); i++ {
		mdl, _ := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyDown})
		m = mdl.(Model)
	}

	rows := m.telemetryRows()
	if got := m.settings.cursor; got != len(core.ValidTimeWindows) {
		t.Fatalf("cursor after %d down presses = %d, want %d", len(core.ValidTimeWindows), got, len(core.ValidTimeWindows))
	}
	if rows[m.settings.cursor].kind != telemetryRowKindUnmapped {
		t.Fatalf("expected cursor to land on an unmapped row, got kind=%v", rows[m.settings.cursor].kind)
	}
}

func TestTelemetryRow_PressingMOpensPickerWithSuggestionPreselected(t *testing.T) {
	m := telemetryFixtureModel()
	// cursor is on github-copilot (first unmapped row)

	mdl, _ := m.handleSettingsModalKey(keyOf("m"))
	m = mdl.(Model)

	if !m.settings.providerLinkPicker.active {
		t.Fatal("expected picker to be active after pressing m")
	}
	if m.settings.providerLinkPicker.source != "github-copilot" {
		t.Fatalf("picker source = %q, want github-copilot", m.settings.providerLinkPicker.source)
	}
	if len(m.settings.providerLinkPicker.choices) == 0 {
		t.Fatal("expected picker choices to be populated from configuredProviderIDs")
	}
	// suggestion was "copilot" — should be preselected
	if got := m.settings.providerLinkPicker.choices[m.settings.providerLinkPicker.cursor]; got != "copilot" {
		t.Fatalf("preselected choice = %q, want copilot", got)
	}
}

func TestTelemetryRow_PickerEnterCallsSaveProviderLink(t *testing.T) {
	fake := &fakeServices{}
	m := telemetryFixtureModel()
	m.services = fake

	mdl, _ := m.handleSettingsModalKey(keyOf("m"))
	m = mdl.(Model)

	mdl, cmd := m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = mdl.(Model)
	if cmd == nil {
		t.Fatal("expected a command from picker enter")
	}
	msg := cmd()
	persisted, ok := msg.(providerLinkPersistedMsg)
	if !ok {
		t.Fatalf("expected providerLinkPersistedMsg, got %T", msg)
	}
	if fake.savedSource != "github-copilot" || fake.savedTarget != "copilot" {
		t.Fatalf("SaveProviderLink called with (%q, %q), want (github-copilot, copilot)", fake.savedSource, fake.savedTarget)
	}
	if persisted.err != nil {
		t.Fatalf("unexpected persistence error: %v", persisted.err)
	}
}

func TestTelemetryRow_PickerEscClosesWithoutSaving(t *testing.T) {
	fake := &fakeServices{}
	m := telemetryFixtureModel()
	m.services = fake

	mdl, _ := m.handleSettingsModalKey(keyOf("m"))
	m = mdl.(Model)
	mdl, _ = m.handleSettingsModalKey(tea.KeyMsg{Type: tea.KeyEsc})
	m = mdl.(Model)

	if m.settings.providerLinkPicker.active {
		t.Fatal("expected picker to be closed after esc")
	}
	if fake.savedSource != "" {
		t.Fatalf("did not expect SaveProviderLink to be called, got %q", fake.savedSource)
	}
}

func TestTelemetryRow_PressingXClearsExistingMapping(t *testing.T) {
	fake := &fakeServices{}
	m := telemetryFixtureModel()
	m.services = fake

	mdl, cmd := m.handleSettingsModalKey(keyOf("x"))
	m = mdl.(Model)
	if cmd == nil {
		t.Fatal("expected a command from x press")
	}
	msg := cmd()
	deleted, ok := msg.(providerLinkDeletedMsg)
	if !ok {
		t.Fatalf("expected providerLinkDeletedMsg, got %T", msg)
	}
	if fake.deletedSrc != "github-copilot" {
		t.Fatalf("DeleteProviderLink called with %q, want github-copilot", fake.deletedSrc)
	}
	if deleted.err != nil {
		t.Fatalf("unexpected deletion error: %v", deleted.err)
	}
}
