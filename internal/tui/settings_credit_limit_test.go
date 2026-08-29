package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

type creditLimitServices struct {
	fakeServices
	accountID string
	limit     *float64
}

func (s *creditLimitServices) SaveAccountCreditLimitOverride(accountID string, limit *float64) error {
	s.accountID = accountID
	s.limit = cloneOptionalFloat(limit)
	return nil
}

func creditLimitModel() Model {
	m := NewModel(80, 90, false, config.DashboardConfig{}, []core.AccountConfig{{ID: "codex-cli", Provider: "codex"}}, core.TimeWindow7d)
	m.settings.show = true
	m.settings.tab = settingsTabProviders
	return m
}

func TestSettingsCreditLimitEditAndSave(t *testing.T) {
	m := creditLimitModel()
	services := &creditLimitServices{}
	m.SetServices(services)

	next, cmd, handled := m.handleSettingsTabProvidersKey(keyOf("l"), m.settingsIDs())
	if !handled || !next.settings.creditLimitEditing {
		t.Fatal("expected l to open the Codex cap editor")
	}
	updated, _ := next.handleCreditLimitEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("4000")})
	next = updated.(Model)
	updated, cmd = next.handleCreditLimitEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	next = updated.(Model)
	if cmd == nil {
		t.Fatal("expected save command")
	}
	msg := cmd()
	result, _ := next.Update(msg)
	next = result.(Model)
	if services.accountID != "codex-cli" || services.limit == nil || *services.limit != 4000 {
		t.Fatalf("unexpected persisted cap: account=%q limit=%v", services.accountID, services.limit)
	}
	if next.accountCreditLimits["codex-cli"] == nil || *next.accountCreditLimits["codex-cli"] != 4000 {
		t.Fatalf("expected model cap to update, got %+v", next.accountCreditLimits)
	}
}

func TestSettingsCreditLimitEmptyInputClears(t *testing.T) {
	cap := 4000.0
	m := creditLimitModel()
	m.accountCreditLimits["codex-cli"] = &cap
	services := &creditLimitServices{}
	m.SetServices(services)
	m.settings.creditLimitEditing = true
	m.settings.creditLimitEditAccountID = "codex-cli"

	nextModel, cmd := m.handleCreditLimitEditKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected clear command")
	}
	next := nextModel.(Model)
	result, _ := next.Update(cmd())
	next = result.(Model)
	if services.limit != nil || next.accountCreditLimits["codex-cli"] != nil {
		t.Fatalf("expected cap to clear, service=%v model=%v", services.limit, next.accountCreditLimits["codex-cli"])
	}
}

func TestSettingsCreditLimitRenderedForCodex(t *testing.T) {
	cap := 4000.0
	m := creditLimitModel()
	m.accountCreditLimits["codex-cli"] = &cap
	body := m.renderSettingsProvidersBody(90, 20)
	if !strings.Contains(body, "CAP") || !strings.Contains(body, "4000") {
		t.Fatalf("expected cap column and value, got:\n%s", body)
	}
}
