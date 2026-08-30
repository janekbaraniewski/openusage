package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
)

type mockUsageModeService struct {
	savedMode string
}

func (s *mockUsageModeService) SaveTheme(themeName string) error { return nil }
func (s *mockUsageModeService) SaveDashboardProviders(providers []config.DashboardProviderConfig) error {
	return nil
}
func (s *mockUsageModeService) SaveDashboardProviderHideCosts(accountID string, hide *bool) error {
	return nil
}
func (s *mockUsageModeService) SaveDashboardView(view string) error { return nil }
func (s *mockUsageModeService) SaveDashboardUsageMode(mode string) error {
	s.savedMode = mode
	return nil
}
func (s *mockUsageModeService) SaveDashboardWidgetSections(sections []config.DashboardWidgetSection) error {
	return nil
}
func (s *mockUsageModeService) SaveDetailWidgetSections(sections []config.DetailWidgetSection) error {
	return nil
}
func (s *mockUsageModeService) SaveDashboardHideSectionsWithNoData(hide bool) error { return nil }
func (s *mockUsageModeService) SaveTimeWindow(window string) error                  { return nil }
func (s *mockUsageModeService) SaveProviderLink(source, target string) error        { return nil }
func (s *mockUsageModeService) DeleteProviderLink(source string) error              { return nil }
func (s *mockUsageModeService) ConnectBrowserSession(accountID, domain, cookieName, preferredBrowser string) (core.BrowserSessionInfo, error) {
	return core.BrowserSessionInfo{}, nil
}
func (s *mockUsageModeService) DisconnectBrowserSession(accountID string) error { return nil }
func (s *mockUsageModeService) LoadBrowserSessionInfo(accountID string) core.BrowserSessionInfo {
	return core.BrowserSessionInfo{}
}
func (s *mockUsageModeService) OpenProviderConsole(url string) error { return nil }
func (s *mockUsageModeService) AvailableBrowsers() ([]string, error) { return nil, nil }
func (s *mockUsageModeService) ValidateAPIKey(accountID, providerID, apiKey string) (bool, string) {
	return true, ""
}
func (s *mockUsageModeService) SaveCredential(accountID, apiKey string) error { return nil }
func (s *mockUsageModeService) DeleteCredential(accountID string) error       { return nil }
func (s *mockUsageModeService) InstallIntegration(id integrations.ID) ([]integrations.Status, error) {
	return nil, nil
}

func TestUsageModeToggle_KeybindingAndPersistence(t *testing.T) {
	accounts := []core.AccountConfig{
		{ID: "antigravity", Provider: "antigravity"},
	}
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, accounts, core.TimeWindow30d)
	m.width = 100
	m.height = 35

	mockSvc := &mockUsageModeService{}
	m.SetServices(mockSvc)

	eighty := 80.0
	now := time.Now()
	snaps := map[string]core.UsageSnapshot{
		"antigravity": {
			AccountID:  "antigravity",
			ProviderID: "antigravity",
			Status:     core.StatusOK,
			Timestamp:  now,
			Metrics: map[string]core.Metric{
				"quota_gemini_weekly": {Remaining: &eighty},
				"quota_gemini_5h":     {Remaining: &eighty},
			},
			Resets: map[string]time.Time{
				"quota_gemini_weekly": now.Add(24 * time.Hour),
				"quota_gemini_5h":     now.Add(2 * time.Hour),
			},
		},
	}
	mUpdated, _ := m.Update(SnapshotsMsg{
		Snapshots:  snaps,
		TimeWindow: core.TimeWindow30d,
	})
	m = mUpdated.(Model)

	// Initial default should be remaining mode
	if m.isUsageModeUsed() {
		t.Fatal("expected default usage mode to be remaining (not used)")
	}
	if !strings.Contains(m.View(), "Remaining") {
		t.Errorf("expected view header to contain 'Remaining', got: %s", m.View())
	}
	if !strings.Contains(m.View(), "Weekly Limit Remaining") {
		t.Errorf("expected 'Weekly Limit Remaining' in remaining mode")
	}
	if !strings.Contains(m.View(), "80.00% remaining") {
		t.Errorf("expected '80.00%% remaining' in remaining mode")
	}

	// Press 'u' to toggle to Used mode
	mUpdated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("u")})
	m = mUpdated.(Model)

	if !m.isUsageModeUsed() {
		t.Fatal("expected isUsageModeUsed() to be true after pressing 'u'")
	}
	if cmd == nil {
		t.Fatal("expected persist command after toggling usage mode")
	}

	// Execute command to verify persistence call
	msg := cmd()
	if mockSvc.savedMode != config.UsageModeUsed {
		t.Errorf("expected saved mode %q, got %q", config.UsageModeUsed, mockSvc.savedMode)
	}

	// Apply persisted message to model
	mUpdated, _ = m.Update(msg)
	m = mUpdated.(Model)

	// Verify view now shows Used labels
	usedView := m.View()
	if !strings.Contains(usedView, "Used") {
		t.Errorf("expected view header to contain 'Used', got: %s", usedView)
	}
	if !strings.Contains(usedView, "Weekly Limit Used") {
		t.Errorf("expected 'Weekly Limit Used' in used mode, got view: %s", usedView)
	}
	if !strings.Contains(usedView, "20.00% used") {
		t.Errorf("expected '20.00%% used' in used mode, got view: %s", usedView)
	}

	// Press 'U' (Shift+U) to toggle back to Remaining mode
	mUpdated, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("U")})
	m = mUpdated.(Model)

	if m.isUsageModeUsed() {
		t.Fatal("expected isUsageModeUsed() to be false after pressing 'U'")
	}
	if cmd == nil {
		t.Fatal("expected persist command after pressing 'U'")
	}
	msg = cmd()
	if mockSvc.savedMode != config.UsageModeRemaining {
		t.Errorf("expected saved mode %q, got %q", config.UsageModeRemaining, mockSvc.savedMode)
	}

	mUpdated, _ = m.Update(msg)
	m = mUpdated.(Model)

	// Verify view returned to Remaining
	if !strings.Contains(m.View(), "Weekly Limit Remaining") {
		t.Errorf("expected 'Weekly Limit Remaining' after toggling back")
	}
}

func TestUsageMode_DetailAndGenericProviderGauges(t *testing.T) {
	now := time.Now()
	usedVal := 35.0
	limitVal := 100.0

	snap := core.UsageSnapshot{
		AccountID:  "custom-provider",
		ProviderID: "cursor",
		Status:     core.StatusOK,
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"plan_percent_used": {
				Used:  &usedVal,
				Limit: &limitVal,
			},
		},
	}

	// Detail in remaining mode
	detailRemaining := RenderDetailContent(snap, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if !strings.Contains(detailRemaining, "65.00% remaining") {
		t.Errorf("expected detail remaining to contain '65.00%% remaining', got:\n%s", detailRemaining)
	}
	if !strings.Contains(detailRemaining, "Included") {
		t.Errorf("expected detail remaining to contain 'Included', got:\n%s", detailRemaining)
	}

	// Detail in used mode
	detailUsed := RenderDetailContent(snap, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeUsed)
	if !strings.Contains(detailUsed, "35.00% used") {
		t.Errorf("expected detail used to contain '35.00%% used', got:\n%s", detailUsed)
	}
	if !strings.Contains(detailUsed, "Included") {
		t.Errorf("expected detail used to contain 'Included', got:\n%s", detailUsed)
	}
}

func TestUsageMode_OpenCodeAndCommandCodeGauges(t *testing.T) {
	now := time.Now()
	sixty := 60.0
	snapOpenCode := core.UsageSnapshot{
		AccountID:  "opencode-test",
		ProviderID: "opencode",
		Status:     core.StatusOK,
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: &sixty},
		},
		Resets: map[string]time.Time{
			"rolling_usage": now.Add(3 * time.Hour),
		},
	}

	detailOCRem := RenderDetailContent(snapOpenCode, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if !strings.Contains(detailOCRem, "Five Hour Limit Remaining") || !strings.Contains(detailOCRem, "60.00% remaining") {
		t.Errorf("expected OpenCode remaining detail labels, got:\n%s", detailOCRem)
	}

	detailOCUsed := RenderDetailContent(snapOpenCode, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeUsed)
	if !strings.Contains(detailOCUsed, "Five Hour Limit Used") || !strings.Contains(detailOCUsed, "40.00% used") {
		t.Errorf("expected OpenCode used detail labels, got:\n%s", detailOCUsed)
	}

	snapCC := core.UsageSnapshot{
		AccountID:  "command_code-test",
		ProviderID: "command_code",
		Status:     core.StatusOK,
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"five_hour_usage": {Remaining: &sixty},
		},
		Resets: map[string]time.Time{
			"five_hour_usage": now.Add(4 * time.Hour),
		},
	}

	detailCCRem := RenderDetailContent(snapCC, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)
	if !strings.Contains(detailCCRem, "Five Hour Limit Remaining") || !strings.Contains(detailCCRem, "60.00% remaining") {
		t.Errorf("expected CommandCode remaining detail labels, got:\n%s", detailCCRem)
	}

	detailCCUsed := RenderDetailContent(snapCC, now, 80, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeUsed)
	if !strings.Contains(detailCCUsed, "Five Hour Limit Used") || !strings.Contains(detailCCUsed, "40.00% used") {
		t.Errorf("expected CommandCode used detail labels, got:\n%s", detailCCUsed)
	}
}

func TestUsageMode_HelpOverlayActionKey(t *testing.T) {
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.width = 100
	m.height = 35
	m.showHelp = true

	v := m.renderHelpOverlay(100, 35)
	if !strings.Contains(v, "u / Shift+U") || !strings.Contains(v, "Toggle usage mode") {
		t.Errorf("expected help overlay to describe u keybinding, got:\n%s", v)
	}
}
