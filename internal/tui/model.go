package tui

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/samber/lo"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

type tickMsg time.Time

func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

type screenTab int

const (
	screenDashboard screenTab = iota // tiles grid overview
	screenAnalytics                  // spend analysis dashboard
)

var screenLabelByTab = map[screenTab]string{
	screenDashboard: "Dashboard",
	screenAnalytics: "Analytics",
}

type viewMode int

const (
	modeList   viewMode = iota // navigating the provider list (left panel focus)
	modeDetail                 // scrolling the detail panel (right panel focus)
)

const (
	minLeftWidth = 28
	maxLeftWidth = 38
)

type SnapshotsMsg map[string]core.UsageSnapshot

type Model struct {
	snapshots map[string]core.UsageSnapshot
	sortedIDs []string
	cursor    int
	mode      viewMode
	filter    string
	filtering bool
	showHelp  bool
	width     int
	height    int

	detailOffset          int // vertical scroll offset for the detail panel
	detailTab             int // active tab index in the detail panel (0=All)
	tileOffset            int // vertical scroll offset for selected dashboard tile row
	expandedModelMixTiles map[string]bool

	warnThreshold float64
	critThreshold float64

	screen screenTab

	analyticsFilter    string
	analyticsFiltering bool
	analyticsSortBy    int // 0=cost↓, 1=name↑, 2=tokens↓

	animFrame  int  // monotonically increasing frame counter
	refreshing bool // true when a manual refresh is in progress
	hasData    bool // true after the first SnapshotsMsg arrives

	experimentalAnalytics bool // when false, only the Dashboard screen is available

	providerOrder       []string
	providerEnabled     map[string]bool
	accountProviders    map[string]string
	showSettingsModal   bool
	settingsModalTab    settingsModalTab
	settingsCursor      int
	settingsThemeCursor int
	settingsStatus      string
	integrationStatuses []integrations.Status

	apiKeyEditing       bool
	apiKeyInput         string
	apiKeyEditAccountID string
	apiKeyStatus        string // "validating...", "valid ✓", "invalid ✗", etc.

	// onAddAccount is called when a new provider account is added from the API Keys tab.
	// Set from main.go to wire into the engine.
	onAddAccount func(core.AccountConfig)
	onRefresh    func()
}

func NewModel(
	warnThresh, critThresh float64,
	experimentalAnalytics bool,
	dashboardCfg config.DashboardConfig,
	accounts []core.AccountConfig,
) Model {
	model := Model{
		snapshots:             make(map[string]core.UsageSnapshot),
		warnThreshold:         warnThresh,
		critThreshold:         critThresh,
		experimentalAnalytics: experimentalAnalytics,
		providerEnabled:       make(map[string]bool),
		accountProviders:      make(map[string]string),
		expandedModelMixTiles: make(map[string]bool),
	}

	model.applyDashboardConfig(dashboardCfg, accounts)
	return model
}

// SetOnAddAccount sets a callback invoked when a new provider account is added via the API Keys tab.
func (m *Model) SetOnAddAccount(fn func(core.AccountConfig)) {
	m.onAddAccount = fn
}

// SetOnRefresh sets a callback invoked when the user requests a manual refresh.
func (m *Model) SetOnRefresh(fn func()) {
	m.onRefresh = fn
}

type themePersistedMsg struct {
	err error
}
type dashboardPrefsPersistedMsg struct {
	err error
}

type validateKeyResultMsg struct {
	AccountID string
	Valid     bool
	Error     string
}

type credentialSavedMsg struct {
	AccountID string
	Err       error
}

type credentialDeletedMsg struct {
	AccountID string
	Err       error
}

type integrationInstallResultMsg struct {
	IntegrationID integrations.ID
	Statuses      []integrations.Status
	Err           error
}

func (m Model) persistThemeCmd(themeName string) tea.Cmd {
	return func() tea.Msg {
		err := config.SaveTheme(themeName)
		if err != nil {
			log.Printf("theme persist: %v", err)
		}
		return themePersistedMsg{err: err}
	}
}

func (m Model) persistDashboardPrefsCmd() tea.Cmd {
	providers := m.dashboardConfigProviders()
	return func() tea.Msg {
		err := config.SaveDashboardProviders(providers)
		if err != nil {
			log.Printf("dashboard settings persist: %v", err)
		}
		return dashboardPrefsPersistedMsg{err: err}
	}
}

func (m Model) validateKeyCmd(accountID, providerID, apiKey string) tea.Cmd {
	return func() tea.Msg {
		var provider core.UsageProvider
		for _, p := range providers.AllProviders() {
			if p.ID() == providerID {
				provider = p
				break
			}
		}
		if provider == nil {
			return validateKeyResultMsg{AccountID: accountID, Valid: false, Error: "unknown provider"}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		acct := core.AccountConfig{
			ID:       accountID,
			Provider: providerID,
			Token:    apiKey,
		}
		snap, err := provider.Fetch(ctx, acct)
		if err != nil {
			return validateKeyResultMsg{AccountID: accountID, Valid: false, Error: err.Error()}
		}
		if snap.Status == core.StatusAuth || snap.Status == core.StatusError {
			msg := snap.Message
			if msg == "" {
				msg = string(snap.Status)
			}
			return validateKeyResultMsg{AccountID: accountID, Valid: false, Error: msg}
		}
		return validateKeyResultMsg{AccountID: accountID, Valid: true}
	}
}

func (m Model) saveCredentialCmd(accountID, apiKey string) tea.Cmd {
	return func() tea.Msg {
		err := config.SaveCredential(accountID, apiKey)
		return credentialSavedMsg{AccountID: accountID, Err: err}
	}
}

func (m Model) deleteCredentialCmd(accountID string) tea.Cmd {
	return func() tea.Msg {
		err := config.DeleteCredential(accountID)
		return credentialDeletedMsg{AccountID: accountID, Err: err}
	}
}

func (m Model) installIntegrationCmd(id integrations.ID) tea.Cmd {
	return func() tea.Msg {
		manager := integrations.NewDefaultManager()
		err := manager.Install(id)
		return integrationInstallResultMsg{
			IntegrationID: id,
			Statuses:      manager.ListStatuses(),
			Err:           err,
		}
	}
}

func (m Model) Init() tea.Cmd { return tickCmd() }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		m.animFrame++
		return m, tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case SnapshotsMsg:
		m.snapshots = msg
		m.refreshing = false
		if len(msg) > 0 || snapshotsReady(msg) {
			m.hasData = true
		}
		m.ensureSnapshotProvidersKnown()
		m.rebuildSortedIDs()
		return m, nil

	case dashboardPrefsPersistedMsg:
		if msg.err != nil {
			m.settingsStatus = "save failed"
		} else {
			m.settingsStatus = "saved"
		}
		return m, nil

	case themePersistedMsg:
		if msg.err != nil {
			m.settingsStatus = "theme save failed"
		} else {
			m.settingsStatus = "theme saved"
		}
		return m, nil

	case validateKeyResultMsg:
		if msg.Valid {
			m.apiKeyStatus = "valid ✓ — saving..."
			return m, m.saveCredentialCmd(msg.AccountID, m.apiKeyInput)
		}
		m.apiKeyStatus = "invalid ✗"
		if msg.Error != "" {
			errMsg := msg.Error
			if len(errMsg) > 40 {
				errMsg = errMsg[:37] + "..."
			}
			m.apiKeyStatus = "invalid: " + errMsg
		}
		return m, nil

	case credentialSavedMsg:
		if msg.Err != nil {
			m.apiKeyStatus = "save failed"
		} else {
			m.apiKeyStatus = "saved ✓"
			apiKey := m.apiKeyInput
			m.apiKeyEditing = false
			m.apiKeyInput = ""

			// Register account with engine if callback is set
			if m.onAddAccount != nil {
				providerID := m.accountProviders[msg.AccountID]
				acct := core.AccountConfig{
					ID:       msg.AccountID,
					Provider: providerID,
					Auth:     "api_key",
					Token:    apiKey,
				}
				m.onAddAccount(acct)
			}

			// Ensure the provider shows in the UI
			if m.providerOrderIndex(msg.AccountID) < 0 {
				m.providerOrder = append(m.providerOrder, msg.AccountID)
				m.providerEnabled[msg.AccountID] = true
			}
			m.refreshing = true
		}
		return m, nil

	case credentialDeletedMsg:
		if msg.Err != nil {
			m.settingsStatus = "delete failed"
		} else {
			m.settingsStatus = "key deleted"
		}
		return m, nil

	case integrationInstallResultMsg:
		m.integrationStatuses = msg.Statuses
		if msg.Err != nil {
			errMsg := msg.Err.Error()
			if len(errMsg) > 80 {
				errMsg = errMsg[:77] + "..."
			}
			m.settingsStatus = "integration install failed: " + errMsg
		} else {
			m.settingsStatus = "integration installed"
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	case tea.MouseMsg:
		return m.handleMouse(msg)
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.showHelp || m.showSettingsModal {
		return m, nil
	}
	if m.filtering || m.analyticsFiltering {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	scroll := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		scroll = -m.mouseScrollStep()
	case tea.MouseButtonWheelDown:
		scroll = m.mouseScrollStep()
	default:
		return m, nil
	}

	if m.screen != screenDashboard {
		return m, nil
	}

	if m.mode == modeDetail {
		m.detailOffset += scroll
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
		return m, nil
	}

	if m.mode == modeList && m.tileCols() == 1 {
		m.tileOffset += scroll
		if m.tileOffset < 0 {
			m.tileOffset = 0
		}
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "?" && !m.filtering && !m.analyticsFiltering && !m.showSettingsModal {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	if m.showSettingsModal {
		return m.handleSettingsModalKey(msg)
	}

	if !m.filtering && !m.analyticsFiltering {
		switch msg.String() {
		case ",", "S":
			m.openSettingsModal()
			return m, nil
		case "tab":
			m.screen = m.nextScreen(1)
			m.mode = modeList
			m.detailOffset = 0
			m.tileOffset = 0
			return m, nil
		case "shift+tab":
			m.screen = m.nextScreen(-1)
			m.mode = modeList
			m.detailOffset = 0
			m.tileOffset = 0
			return m, nil
		case "t":
			name := CycleTheme()
			return m, m.persistThemeCmd(name)
		}
	}

	switch m.screen {
	case screenAnalytics:
		return m.handleAnalyticsKey(msg)
	default:
		return m.handleDashboardTilesKey(msg)
	}
}

func (m Model) handleDashboardTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.mode == modeDetail {
		return m.handleDetailKey(msg)
	}
	return m.handleTilesKey(msg)
}

func (m Model) handleAnalyticsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.analyticsFiltering {
		return m.handleAnalyticsFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		m.analyticsSortBy = (m.analyticsSortBy + 1) % analyticsSortCount
	case "/":
		m.analyticsFiltering = true
		m.analyticsFilter = ""
	case "esc":
		if m.analyticsFilter != "" {
			m.analyticsFilter = ""
		}
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}

func (m Model) handleAnalyticsFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.analyticsFiltering = false
	case "esc":
		m.analyticsFiltering = false
		m.analyticsFilter = ""
	case "backspace":
		if len(m.analyticsFilter) > 0 {
			m.analyticsFilter = m.analyticsFilter[:len(m.analyticsFilter)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.analyticsFilter += msg.String()
		}
	}
	return m, nil
}

func (m Model) availableScreens() []screenTab {
	if !m.experimentalAnalytics {
		return []screenTab{screenDashboard}
	}
	return []screenTab{screenDashboard, screenAnalytics}
}

func (m Model) nextScreen(step int) screenTab {
	screens := m.availableScreens()
	if len(screens) == 0 {
		return screenDashboard
	}

	idx := 0
	for i, screen := range screens {
		if screen == m.screen {
			idx = i
			break
		}
	}

	next := (idx + step) % len(screens)
	if next < 0 {
		next += len(screens)
	}
	return screens[next]
}

func (m Model) handleListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.filteredIDs()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.detailOffset = 0
			m.detailTab = 0
			m.tileOffset = 0
		}
	case "down", "j":
		if m.cursor < len(ids)-1 {
			m.cursor++
			m.detailOffset = 0
			m.detailTab = 0
			m.tileOffset = 0
		}
	case "enter", "right", "l":
		m.mode = modeDetail
		m.detailOffset = 0
	case "/":
		m.filtering = true
		m.filter = ""
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "left", "h", "backspace":
		m.mode = modeList
	case "up", "k":
		if m.detailOffset > 0 {
			m.detailOffset--
		}
	case "down", "j":
		m.detailOffset++ // capped during render
	case "g":
		m.detailOffset = 0
	case "G":
		m.detailOffset = 9999 // will be capped
	case "[":
		if m.detailTab > 0 {
			m.detailTab--
			m.detailOffset = 0
		}
	case "]":
		m.detailTab++
		m.detailOffset = 0
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1') // "1" → 0, "2" → 1, ...
		m.detailTab = idx
		m.detailOffset = 0
	}
	return m, nil
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.cursor = 0
		m.tileOffset = 0
	case "esc":
		m.filter = ""
		m.filtering = false
		m.cursor = 0
		m.tileOffset = 0
	case "backspace":
		if len(m.filter) > 0 {
			m.filter = m.filter[:len(m.filter)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.filter += msg.String()
		}
	}
	return m, nil
}

func (m Model) handleTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.filteredIDs()
	cols := m.tileCols()
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.cursor >= cols {
			m.cursor -= cols
			m.tileOffset = 0
		}
	case "down", "j":
		if m.cursor+cols < len(ids) {
			m.cursor += cols
			m.tileOffset = 0
		}
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
			m.tileOffset = 0
		}
	case "right", "l":
		if m.cursor < len(ids)-1 {
			m.cursor++
			m.tileOffset = 0
		}
	case "pgdown", "ctrl+d":
		m.tileOffset += m.tileScrollStep()
	case "pgup", "ctrl+u":
		m.tileOffset -= m.tileScrollStep()
		if m.tileOffset < 0 {
			m.tileOffset = 0
		}
	case "ctrl+o":
		if id := m.selectedTileID(ids); id != "" {
			m.expandedModelMixTiles[id] = !m.expandedModelMixTiles[id]
		}
	case "home":
		m.tileOffset = 0
	case "end":
		m.tileOffset = 9999 // capped during render
	case "enter":
		m.mode = modeDetail
		m.detailOffset = 0
	case "/":
		m.filtering = true
		m.filter = ""
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.cursor = 0
			m.tileOffset = 0
		}
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}

func (m Model) requestRefresh() Model {
	m.refreshing = true
	if m.onRefresh != nil {
		m.onRefresh()
	}
	return m
}

func (m Model) selectedTileID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	if m.cursor < 0 || m.cursor >= len(ids) {
		return ""
	}
	return ids[m.cursor]
}

func (m Model) tileScrollStep() int {
	step := m.height / 4
	if step < 3 {
		step = 3
	}
	return step
}

func (m Model) mouseScrollStep() int {
	step := m.height / 10
	if step < 3 {
		step = 3
	}
	return step
}

func snapshotsReady(snaps map[string]core.UsageSnapshot) bool {
	if len(snaps) == 0 {
		return false
	}
	for _, snap := range snaps {
		if snap.Status != core.StatusUnknown {
			return true
		}
		if len(snap.Metrics) > 0 ||
			len(snap.Resets) > 0 ||
			len(snap.DailySeries) > 0 ||
			len(snap.ModelUsage) > 0 {
			return true
		}
	}
	return false
}

func (m Model) View() string {
	if m.width < 30 || m.height < 8 {
		return lipgloss.NewStyle().
			Foreground(colorDim).
			Render("\n  Terminal too small. Resize to at least 30×8.")
	}
	if !m.hasData {
		return m.renderSplash(m.width, m.height)
	}
	if m.showHelp {
		return m.renderHelpOverlay(m.width, m.height)
	}
	view := m.renderDashboard()
	if m.showSettingsModal {
		return m.renderSettingsModalOverlay()
	}
	return view
}

func (m Model) renderDashboard() string {
	w, h := m.width, m.height

	header := m.renderHeader(w)
	headerH := strings.Count(header, "\n") + 1

	footer := m.renderFooter(w)
	footerH := strings.Count(footer, "\n") + 1

	contentH := h - headerH - footerH
	if contentH < 3 {
		contentH = 3
	}

	var content string

	switch m.screen {
	case screenAnalytics:
		content = m.renderAnalyticsContent(w, contentH)
	default:
		content = m.renderDashboardContent(w, contentH)
	}

	return header + "\n" + content + "\n" + footer
}

func (m Model) renderDashboardContent(w, contentH int) string {
	if m.mode == modeDetail {
		return m.renderDetailPanel(w, contentH)
	}
	return m.renderTiles(w, contentH)
}

func (m Model) renderHeader(w int) string {
	bolt := PulseChar(
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("⚡"),
		lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("⚡"),
		m.animFrame,
	)
	brandText := RenderGradientText("OpenUsage", m.animFrame)

	tabs := m.renderScreenTabs()

	spinnerStr := ""
	if m.refreshing {
		frame := m.animFrame % len(SpinnerFrames)
		spinnerStr = " " + lipgloss.NewStyle().Foreground(colorAccent).Render(SpinnerFrames[frame])
	}

	ids := m.filteredIDs()
	unmappedProviders := m.telemetryUnmappedProviders()

	okCount, warnCount, errCount := 0, 0, 0
	for _, id := range ids {
		snap := m.snapshots[id]
		switch snap.Status {
		case core.StatusOK:
			okCount++
		case core.StatusNearLimit:
			warnCount++
		case core.StatusLimited, core.StatusError:
			errCount++
		}
	}

	var info string

	if m.showSettingsModal {
		info = m.settingsModalInfo()
	} else {
		switch m.screen {
		case screenAnalytics:
			info = dimStyle.Render("spend analysis")
			if m.analyticsFilter != "" {
				info += " (filtered)"
			}
		default:
			info = fmt.Sprintf("⊞ %d providers", len(ids))
			if m.filter != "" {
				info += " (filtered)"
			}
		}
	}
	if !m.showSettingsModal && len(unmappedProviders) > 0 {
		info += " · detected additional providers, check settings"
	}

	statusInfo := ""
	if okCount > 0 {
		dot := PulseChar("●", "◉", m.animFrame)
		statusInfo += lipgloss.NewStyle().Foreground(colorGreen).Render(fmt.Sprintf(" %d%s", okCount, dot))
	}
	if warnCount > 0 {
		dot := PulseChar("◐", "◑", m.animFrame)
		statusInfo += lipgloss.NewStyle().Foreground(colorYellow).Render(fmt.Sprintf(" %d%s", warnCount, dot))
	}
	if errCount > 0 {
		dot := PulseChar("✗", "✕", m.animFrame)
		statusInfo += lipgloss.NewStyle().Foreground(colorRed).Render(fmt.Sprintf(" %d%s", errCount, dot))
	}
	if len(unmappedProviders) > 0 {
		statusInfo += lipgloss.NewStyle().
			Foreground(colorPeach).
			Render(fmt.Sprintf(" ⚠ %d unmapped", len(unmappedProviders)))
	}

	infoRendered := lipgloss.NewStyle().Foreground(colorSubtext).Render(info)

	left := bolt + " " + brandText + " " + tabs + statusInfo + spinnerStr
	gap := w - lipgloss.Width(left) - lipgloss.Width(infoRendered)
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + infoRendered

	sep := m.renderGradientSeparator(w)

	return line + "\n" + sep
}

func (m Model) renderGradientSeparator(w int) string {
	if w <= 0 {
		return ""
	}
	sepStyle := lipgloss.NewStyle().Foreground(colorSurface1)
	return sepStyle.Render(strings.Repeat("━", w))
}

func (m Model) renderScreenTabs() string {
	screens := m.availableScreens()
	if len(screens) <= 1 {
		return ""
	}
	var parts []string
	for i, screen := range screens {
		label := screenLabelByTab[screen]
		tabStr := fmt.Sprintf("%d:%s", i+1, label)
		if screen == m.screen {
			parts = append(parts, screenTabActiveStyle.Render(tabStr))
		} else {
			parts = append(parts, screenTabInactiveStyle.Render(tabStr))
		}
	}
	return strings.Join(parts, "")
}

func (m Model) renderFooter(w int) string {
	sep := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("━", w))
	statusLine := m.renderFooterStatusLine()
	return sep + "\n" + statusLine
}

func (m Model) renderFooterStatusLine() string {
	searchStyle := lipgloss.NewStyle().Foreground(colorSapphire)

	switch {
	case m.showSettingsModal:
		if m.settingsStatus != "" {
			return " " + dimStyle.Render(m.settingsStatus)
		}
		return " " + helpStyle.Render("? help")
	case m.screen == screenAnalytics:
		if m.analyticsFiltering {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.analyticsFilter+cursor)
		}
		if m.analyticsFilter != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.analyticsFilter)
		}
	default:
		if m.filtering {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.filter+cursor)
		}
		if m.filter != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.filter)
		}
		if m.mode == modeList && m.tileOffset > 0 {
			return " " + dimStyle.Render("panel scroll active · PgUp/PgDn · Home/End")
		}
	}

	return " " + helpStyle.Render("? help")
}

func (m Model) renderList(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  Loading providers…"),
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Fetching usage and spend data."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	itemHeight := 3 // each item is 3 lines (name + summary + separator)
	visibleItems := h / itemHeight
	if visibleItems < 1 {
		visibleItems = 1
	}

	scrollStart := 0
	if m.cursor >= visibleItems {
		scrollStart = m.cursor - visibleItems + 1
	}
	scrollEnd := scrollStart + visibleItems
	if scrollEnd > len(ids) {
		scrollEnd = len(ids)
		scrollStart = scrollEnd - visibleItems
		if scrollStart < 0 {
			scrollStart = 0
		}
	}

	var lines []string
	for i := scrollStart; i < scrollEnd; i++ {
		id := ids[i]
		snap := m.snapshots[id]
		selected := i == m.cursor
		item := m.renderListItem(snap, selected, w)
		lines = append(lines, item)
	}

	if scrollStart > 0 {
		arrow := lipgloss.NewStyle().Foreground(colorDim).Render("  ▲ " + fmt.Sprintf("%d more", scrollStart))
		lines = append([]string{arrow}, lines...)
	}
	if scrollEnd < len(ids) {
		arrow := lipgloss.NewStyle().Foreground(colorDim).Render("  ▼ " + fmt.Sprintf("%d more", len(ids)-scrollEnd))
		lines = append(lines, arrow)
	}

	content := strings.Join(lines, "\n")
	return padToSize(content, w, h)
}

func (m Model) renderListItem(snap core.UsageSnapshot, selected bool, w int) string {
	di := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))

	icon := StatusIcon(snap.Status)
	iconColor := StatusColor(snap.Status)
	iconStr := lipgloss.NewStyle().Foreground(iconColor).Render(icon)

	nameStyle := lipgloss.NewStyle().Foreground(colorText)
	if selected {
		nameStyle = nameStyle.Bold(true).Foreground(colorLavender)
	}

	badge := StatusBadge(snap.Status)
	var tagRendered string
	if di.tagEmoji != "" && di.tagLabel != "" {
		tc := tagColor(di.tagLabel)
		tagRendered = lipgloss.NewStyle().Foreground(tc).Render(di.tagEmoji+" "+di.tagLabel) + " "
	}
	rightPart := tagRendered + badge
	rightW := lipgloss.Width(rightPart)

	name := snap.AccountID
	maxName := w - rightW - 6 // icon + spaces + gap
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	namePart := fmt.Sprintf(" %s %s", iconStr, nameStyle.Render(name))
	nameW := lipgloss.Width(namePart)
	gapLen := w - nameW - rightW - 1
	if gapLen < 1 {
		gapLen = 1
	}
	line1 := namePart + strings.Repeat(" ", gapLen) + rightPart

	summary := di.summary
	summaryStyle := lipgloss.NewStyle().Foreground(colorText).Bold(true)

	miniGauge := ""
	if di.gaugePercent >= 0 && w > 25 {
		gaugeW := 8
		if w < 35 {
			gaugeW = 5
		}
		miniGauge = " " + RenderMiniGauge(di.gaugePercent, gaugeW)
	}

	summaryMaxW := w - 5 - lipgloss.Width(miniGauge)
	if summaryMaxW < 5 {
		summaryMaxW = 5
	}
	if len(summary) > summaryMaxW {
		summary = summary[:summaryMaxW-1] + "…"
	}

	line2 := "   " + summaryStyle.Render(summary) + miniGauge

	line3 := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4))

	result := line1 + "\n" + line2 + "\n" + line3

	if selected {
		indicator := lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("┃")
		rlines := strings.Split(result, "\n")
		for i, l := range rlines {
			if len(l) > 0 {
				rlines[i] = indicator + l[1:]
			}
		}
		result = strings.Join(rlines, "\n")
	}

	return result
}

type providerDisplayInfo struct {
	tagEmoji     string  // "💰", "⚡", "🔑", "⚠", "◇"
	tagLabel     string  // "Credits", "Usage", "Error", "Auth", "N/A"
	summary      string  // Primary summary (e.g. "$4.23 today · $0.82/h")
	detail       string  // Secondary detail (e.g. "Primary 3% · Secondary 15%")
	gaugePercent float64 // 0-100 used %. -1 if not applicable.
}

func computeDisplayInfo(snap core.UsageSnapshot, widget core.DashboardWidget) providerDisplayInfo {
	return normalizeProviderDisplayInfoType(computeDisplayInfoRaw(snap, widget))
}

func normalizeProviderDisplayInfoType(info providerDisplayInfo) providerDisplayInfo {
	switch info.tagLabel {
	case "Credits":
		info.tagEmoji = "💰"
	case "Usage":
		info.tagEmoji = "⚡"
	case "Error", "Auth", "N/A", "":
		// Status and empty labels are allowed as-is.
	default:
		// Enforce only two billing types for provider tags.
		info.tagLabel = "Usage"
		info.tagEmoji = "⚡"
	}
	return info
}

func computeDisplayInfoRaw(snap core.UsageSnapshot, widget core.DashboardWidget) providerDisplayInfo {
	info := providerDisplayInfo{gaugePercent: -1}

	switch snap.Status {
	case core.StatusError:
		info.tagEmoji = "⚠"
		info.tagLabel = "Error"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		if msg == "" {
			msg = "Error"
		}
		info.summary = msg
		return info
	case core.StatusAuth:
		info.tagEmoji = "🔑"
		info.tagLabel = "Auth"
		info.summary = "Authentication required"
		return info
	case core.StatusUnsupported:
		info.tagEmoji = "◇"
		info.tagLabel = "N/A"
		info.summary = "Not supported"
		return info
	}

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil {
		remaining := *m.Limit - *m.Used
		if m.Remaining != nil {
			remaining = *m.Remaining
		}
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.0f / $%.0f spent", *m.Used, *m.Limit)
		info.detail = fmt.Sprintf("$%.0f remaining", remaining)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}
		return info
	}

	if m, ok := snap.Metrics["plan_spend"]; ok && m.Used != nil && m.Limit != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.0f / $%.0f plan", *m.Used, *m.Limit)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}
		if pu, ok2 := snap.Metrics["plan_percent_used"]; ok2 && pu.Used != nil {
			info.detail = fmt.Sprintf("%.0f%% plan used", *pu.Used)
		}
		return info
	}

	if m, ok := snap.Metrics["plan_total_spend_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if lm, ok2 := snap.Metrics["plan_limit_usd"]; ok2 && lm.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.0f plan", *m.Used, *lm.Limit)
		} else {
			info.summary = fmt.Sprintf("$%.2f spent", *m.Used)
		}
		return info
	}

	// Style hooks for richer credit summaries.
	if widget.DisplayStyle == core.DashboardDisplayStyleDetailedCredits {
		return computeDetailedCreditsDisplayInfo(snap, info)
	}

	if m, ok := snap.Metrics["credits"]; ok {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if m.Remaining != nil && m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f credits", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = 100 - pct
			}
		} else if m.Used != nil {
			info.summary = fmt.Sprintf("$%.4f used", *m.Used)
		} else {
			info.summary = "Credits available"
		}
		return info
	}
	if m, ok := snap.Metrics["credit_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		if m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = 100 - pct
			}
		} else {
			info.summary = fmt.Sprintf("$%.2f balance", *m.Remaining)
		}
		return info
	}
	if m, ok := snap.Metrics["total_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("%.2f %s available", *m.Remaining, m.Unit)
		return info
	}

	quotaKey := ""
	for _, key := range []string{"quota_pro", "quota", "quota_flash"} {
		if _, ok := snap.Metrics[key]; ok {
			quotaKey = key
			break
		}
	}
	if quotaKey != "" {
		m := snap.Metrics[quotaKey]
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		if pct := core.MetricUsedPercent(quotaKey, m); pct >= 0 {
			info.gaugePercent = pct
			info.summary = fmt.Sprintf("%.0f%% usage used", pct)
		}
		if m.Remaining != nil {
			info.detail = fmt.Sprintf("%.0f%% usage left", *m.Remaining)
		}
		return info
	}

	if m, ok := snap.Metrics["context_window"]; ok && m.Used != nil && m.Limit != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = pct
			info.summary = fmt.Sprintf("%.0f%% usage used", pct)
		}
		info.detail = fmt.Sprintf("%s / %s tokens", shortCompact(*m.Used), shortCompact(*m.Limit))
		return info
	}

	hasRateLimits := false
	worstRatePct := float64(100)
	var rateParts []string
	for key, m := range snap.Metrics {
		isRate := strings.HasPrefix(key, "rate_limit_") ||
			key == "rpm" || key == "tpm" || key == "rpd" || key == "tpd"
		if !isRate {
			continue
		}
		hasRateLimits = true
		pct := m.Percent()
		if pct >= 0 && pct < worstRatePct {
			worstRatePct = pct
		}
		if m.Unit == "%" && m.Remaining != nil {
			label := metricLabel(widget, strings.TrimPrefix(key, "rate_limit_"))
			rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", label, 100-*m.Remaining))
		} else if pct >= 0 {
			label := strings.ToUpper(key)
			rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", label, 100-pct))
		}
	}
	if hasRateLimits {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.gaugePercent = 100 - worstRatePct
		info.summary = fmt.Sprintf("%.0f%% used", 100-worstRatePct)
		if len(rateParts) > 0 {
			sort.Strings(rateParts)
			info.detail = strings.Join(rateParts, " · ")
		}
		return info
	}

	if fh, ok := snap.Metrics["usage_five_hour"]; ok && fh.Used != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"

		info.gaugePercent = *fh.Used
		parts := []string{fmt.Sprintf("5h %.0f%%", *fh.Used)}

		if sd, ok2 := snap.Metrics["usage_seven_day"]; ok2 && sd.Used != nil {
			parts = append(parts, fmt.Sprintf("7d %.0f%%", *sd.Used))
			if *sd.Used > info.gaugePercent {
				info.gaugePercent = *sd.Used
			}
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if dc, ok2 := snap.Metrics["today_api_cost"]; ok2 && dc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f today", *dc.Used))
		}
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	if m, ok := snap.Metrics["today_api_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		parts := []string{fmt.Sprintf("~$%.2f today", *m.Used)}
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			parts = append(parts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if bc, ok2 := snap.Metrics["5h_block_cost"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f 5h block", *bc.Used))
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f/7d", *wc.Used))
		}
		if msgs, ok2 := snap.Metrics["messages_today"]; ok2 && msgs.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f msgs", *msgs.Used))
		}
		if sess, ok2 := snap.Metrics["sessions_today"]; ok2 && sess.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sess.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	if m, ok := snap.Metrics["5h_block_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("~$%.2f / 5h block", *m.Used)
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			info.detail = fmt.Sprintf("$%.2f/h burn rate", *br.Used)
		}
		return info
	}

	hasUsage := false
	worstUsagePct := float64(100)
	var usageKey string
	usageKeys := sortedMetricKeys(snap.Metrics)
	for _, key := range usageKeys {
		m := snap.Metrics[key]
		pct := m.Percent()
		if pct >= 0 {
			hasUsage = true
			if pct < worstUsagePct {
				worstUsagePct = pct
				usageKey = key
			}
		}
	}
	if hasUsage {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.gaugePercent = 100 - worstUsagePct
		info.summary = fmt.Sprintf("%.0f%% used", 100-worstUsagePct)
		if snap.ProviderID == "gemini_cli" {
			if m, ok := snap.Metrics["total_conversations"]; ok && m.Used != nil {
				info.detail = fmt.Sprintf("%.0f conversations", *m.Used)
				return info
			}
			if m, ok := snap.Metrics["messages_today"]; ok && m.Used != nil {
				info.detail = fmt.Sprintf("%.0f msgs today", *m.Used)
				return info
			}
			return info
		}
		if usageKey != "" {
			qm := snap.Metrics[usageKey]
			parts := []string{metricLabel(widget, usageKey)}
			if qm.Window != "" && qm.Window != "all_time" && qm.Window != "current_period" {
				parts = append(parts, qm.Window)
			}
			info.detail = strings.Join(parts, " · ")
		}
		return info
	}

	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.2f total", *m.Used)
		return info
	}
	if m, ok := snap.Metrics["all_time_api_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("~$%.2f total (API est.)", *m.Used)
		return info
	}

	if m, ok := snap.Metrics["messages_today"]; ok && m.Used != nil {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.summary = fmt.Sprintf("%.0f msgs today", *m.Used)
		var detailParts []string
		if tc, ok2 := snap.Metrics["tool_calls_today"]; ok2 && tc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f tools", *tc.Used))
		}
		if sc, ok2 := snap.Metrics["sessions_today"]; ok2 && sc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sc.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	for _, key := range fallbackDisplayMetricKeys(snap.Metrics) {
		m := snap.Metrics[key]
		if m.Used != nil {
			info.tagEmoji = "⚡"
			info.tagLabel = "Usage"
			info.summary = fmt.Sprintf("%s: %s %s", metricLabel(widget, key), formatNumber(*m.Used), m.Unit)
			return info
		}
	}

	if snap.Message != "" {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		info.summary = msg
		return info
	}

	info.tagEmoji = "⚡"
	info.tagLabel = "Usage"
	if snap.Status == core.StatusUnknown {
		info.summary = "Syncing telemetry..."
	} else {
		info.summary = string(snap.Status)
	}
	return info
}

func fallbackDisplayMetricKeys(metrics map[string]core.Metric) []string {
	keys := sortedMetricKeys(metrics)
	if len(keys) == 0 {
		return nil
	}

	excludePrefixes := []string{
		"model_", "client_", "tool_", "source_",
		"usage_model_", "usage_source_", "usage_client_",
		"tokens_client_", "analytics_",
	}
	filtered := lo.Filter(keys, func(key string, _ int) bool {
		return !lo.SomeBy(excludePrefixes, func(prefix string) bool {
			return strings.HasPrefix(key, prefix)
		})
	})
	if len(filtered) > 0 {
		return filtered
	}
	return keys
}

// computeDetailedCreditsDisplayInfo renders a richer credits summary/detail view
// for providers that expose both balance and usage dimensions.
func computeDetailedCreditsDisplayInfo(snap core.UsageSnapshot, info providerDisplayInfo) providerDisplayInfo {
	metricUsed := func(key string) *float64 {
		if m, ok := snap.Metrics[key]; ok && m.Used != nil {
			return m.Used
		}
		return nil
	}

	// Prefer account-level purchased credits when available.
	if m, ok := snap.Metrics["credit_balance"]; ok && m.Limit != nil && m.Remaining != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		spent := *m.Limit - *m.Remaining
		if m.Used != nil {
			spent = *m.Used
		}
		info.summary = fmt.Sprintf("$%.2f / $%.2f spent", spent, *m.Limit)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}

		detailParts := []string{fmt.Sprintf("$%.2f remaining", *m.Remaining)}
		switch {
		case metricUsed("today_cost") != nil:
			detailParts = append(detailParts, fmt.Sprintf("today $%.2f", *metricUsed("today_cost")))
		case metricUsed("usage_daily") != nil:
			detailParts = append(detailParts, fmt.Sprintf("today $%.2f", *metricUsed("usage_daily")))
		}
		switch {
		case metricUsed("7d_api_cost") != nil:
			detailParts = append(detailParts, fmt.Sprintf("week $%.2f", *metricUsed("7d_api_cost")))
		case metricUsed("usage_weekly") != nil:
			detailParts = append(detailParts, fmt.Sprintf("week $%.2f", *metricUsed("usage_weekly")))
		}
		if models := snapshotMeta(snap, "activity_models"); models != "" {
			detailParts = append(detailParts, fmt.Sprintf("%s models", models))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	// Fallback to key-level credits/usage.
	if m, ok := snap.Metrics["credits"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.summary = fmt.Sprintf("$%.4f used", *m.Used)

		var detailParts []string
		if daily, ok := snap.Metrics["usage_daily"]; ok && daily.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("today $%.2f", *daily.Used))
		}
		if byok, ok := snap.Metrics["byok_daily"]; ok && byok.Used != nil && *byok.Used > 0 {
			detailParts = append(detailParts, fmt.Sprintf("BYOK $%.2f", *byok.Used))
		}
		if burn, ok := snap.Metrics["burn_rate"]; ok && burn.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f/h", *burn.Used))
		}
		if models := snapshotMeta(snap, "activity_models"); models != "" {
			detailParts = append(detailParts, fmt.Sprintf("%s models", models))
		}
		info.detail = strings.Join(detailParts, " · ")
		return info
	}

	// Fallback to generic
	info.tagEmoji = "💰"
	info.tagLabel = "Credits"
	info.summary = "Connected"
	return info
}

func providerSummary(snap core.UsageSnapshot) string {
	return computeDisplayInfo(snap, dashboardWidget(snap.ProviderID)).summary
}

func bestMetricPercent(snap core.UsageSnapshot) float64 {
	hasSpendLimit := false
	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && *m.Limit > 0 {
		hasSpendLimit = true
	}

	worstRemaining := float64(100)
	found := false
	for key, m := range snap.Metrics {
		if hasSpendLimit && (key == "plan_percent_used" || key == "plan_spend") {
			continue
		}
		p := m.Percent()
		if p >= 0 {
			found = true
			if p < worstRemaining {
				worstRemaining = p
			}
		}
	}
	if !found {
		return -1
	}
	return 100 - worstRemaining
}

func (m Model) renderDetailPanel(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || m.cursor >= len(ids) {
		return padToSize("", w, h)
	}

	snap := m.snapshots[ids[m.cursor]]

	tabs := DetailTabs(snap)
	activeTab := m.detailTab
	if activeTab >= len(tabs) {
		activeTab = len(tabs) - 1
	}
	if activeTab < 0 {
		activeTab = 0
	}

	content := RenderDetailContent(snap, w-2, m.warnThreshold, m.critThreshold, activeTab)

	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	offset := m.detailOffset
	if offset > totalLines-h {
		offset = totalLines - h
	}
	if offset < 0 {
		offset = 0
	}

	end := offset + h
	if end > totalLines {
		end = totalLines
	}

	visible := lines[offset:end]

	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")

	if m.mode == modeDetail {
		rlines := strings.Split(result, "\n")
		if offset > 0 && len(rlines) > 0 {
			arrow := lipgloss.NewStyle().Foreground(colorAccent).Render("  ▲ scroll up")
			rlines[0] = arrow
		}
		if end < totalLines && len(rlines) > 1 {
			arrow := lipgloss.NewStyle().Foreground(colorAccent).Render("  ▼ more below")
			rlines[len(rlines)-1] = arrow
		}
		result = strings.Join(rlines, "\n")
	}

	return lipgloss.NewStyle().Width(w).Padding(0, 1).Render(result)
}

func renderVerticalSep(h int) string {
	style := lipgloss.NewStyle().Foreground(colorSurface1)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = style.Render("┃")
	}
	return strings.Join(lines, "\n")
}

func (m *Model) applyDashboardConfig(dashboardCfg config.DashboardConfig, accounts []core.AccountConfig) {
	accountOrder := make([]string, 0, len(accounts))
	seenAccounts := make(map[string]bool, len(accounts))

	for _, account := range accounts {
		if account.ID == "" {
			continue
		}
		if !seenAccounts[account.ID] {
			accountOrder = append(accountOrder, account.ID)
			seenAccounts[account.ID] = true
		}
		m.accountProviders[account.ID] = account.Provider
		m.providerEnabled[account.ID] = true
	}

	order := make([]string, 0, len(accountOrder))
	seen := make(map[string]bool, len(accountOrder))
	for _, pref := range dashboardCfg.Providers {
		id := pref.AccountID
		if id == "" || seen[id] || !seenAccounts[id] {
			continue
		}
		seen[id] = true
		m.providerEnabled[id] = pref.Enabled
		order = append(order, id)
	}

	for _, id := range accountOrder {
		if seen[id] {
			continue
		}
		order = append(order, id)
	}

	m.providerOrder = order
}

func (m *Model) ensureSnapshotProvidersKnown() {
	if len(m.snapshots) == 0 {
		return
	}
	keys := lo.Keys(m.snapshots)
	sort.Strings(keys)

	for _, id := range keys {
		if m.providerOrderIndex(id) >= 0 {
			if m.accountProviders[id] == "" {
				m.accountProviders[id] = m.snapshots[id].ProviderID
			}
			continue
		}
		m.providerOrder = append(m.providerOrder, id)
		if _, ok := m.providerEnabled[id]; !ok {
			m.providerEnabled[id] = true
		}
		if m.accountProviders[id] == "" {
			m.accountProviders[id] = m.snapshots[id].ProviderID
		}
	}
}

func (m Model) providerOrderIndex(id string) int {
	for i, providerID := range m.providerOrder {
		if providerID == id {
			return i
		}
	}
	return -1
}

func (m Model) settingsIDs() []string {
	ids := make([]string, len(m.providerOrder))
	copy(ids, m.providerOrder)
	return ids
}

func (m Model) telemetryUnmappedProviders() []string {
	seen := make(map[string]bool)
	for _, snap := range m.snapshots {
		raw := strings.TrimSpace(snap.Diagnostics["telemetry_unmapped_providers"])
		if raw == "" {
			continue
		}
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			seen[token] = true
		}
	}

	out := lo.Keys(seen)
	sort.Strings(out)
	return out
}

func (m Model) telemetryProviderLinkHints() []string {
	seen := make(map[string]bool)
	for _, snap := range m.snapshots {
		hint := strings.TrimSpace(snap.Diagnostics["telemetry_provider_link_hint"])
		if hint == "" {
			continue
		}
		seen[hint] = true
	}

	out := lo.Keys(seen)
	sort.Strings(out)
	return out
}

func (m Model) configuredProviderIDs() []string {
	seen := make(map[string]bool)

	for _, providerID := range m.accountProviders {
		providerID = strings.TrimSpace(providerID)
		if providerID == "" {
			continue
		}
		seen[providerID] = true
	}
	for _, snap := range m.snapshots {
		providerID := strings.TrimSpace(snap.ProviderID)
		if providerID == "" {
			continue
		}
		seen[providerID] = true
	}

	out := lo.Keys(seen)
	sort.Strings(out)
	return out
}

func (m *Model) refreshIntegrationStatuses() {
	manager := integrations.NewDefaultManager()
	m.integrationStatuses = manager.ListStatuses()
}

func (m Model) dashboardConfigProviders() []config.DashboardProviderConfig {
	ids := m.settingsIDs()
	out := make([]config.DashboardProviderConfig, 0, len(ids))
	for _, id := range ids {
		out = append(out, config.DashboardProviderConfig{
			AccountID: id,
			Enabled:   m.isProviderEnabled(id),
		})
	}
	return out
}

func (m Model) isProviderEnabled(id string) bool {
	enabled, ok := m.providerEnabled[id]
	if !ok {
		return true
	}
	return enabled
}

func (m Model) visibleSnapshots() map[string]core.UsageSnapshot {
	out := make(map[string]core.UsageSnapshot, len(m.snapshots))
	for id, snap := range m.snapshots {
		if m.isProviderEnabled(id) {
			out[id] = snap
		}
	}
	return out
}

func (m *Model) rebuildSortedIDs() {
	ordered := make([]string, 0, len(m.snapshots))
	seen := make(map[string]bool, len(m.snapshots))

	for _, id := range m.providerOrder {
		if !m.isProviderEnabled(id) {
			continue
		}
		if _, ok := m.snapshots[id]; !ok {
			continue
		}
		ordered = append(ordered, id)
		seen[id] = true
	}

	extra := lo.Filter(lo.Keys(m.snapshots), func(id string, _ int) bool {
		return !seen[id] && m.isProviderEnabled(id)
	})
	sort.Strings(extra)

	m.sortedIDs = append(ordered, extra...)
	if m.cursor >= len(m.sortedIDs) {
		m.cursor = len(m.sortedIDs) - 1
		if m.cursor < 0 {
			m.cursor = 0
		}
	}
}

func (m Model) filteredIDs() []string {
	if m.filter == "" {
		return m.sortedIDs
	}
	lower := strings.ToLower(m.filter)
	return lo.Filter(m.sortedIDs, func(id string, _ int) bool {
		snap := m.snapshots[id]
		return strings.Contains(strings.ToLower(id), lower) ||
			strings.Contains(strings.ToLower(snap.ProviderID), lower) ||
			strings.Contains(strings.ToLower(string(snap.Status)), lower)
	})
}

func padToSize(content string, w, h int) string {
	lines := strings.Split(content, "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	if len(lines) > h {
		lines = lines[:h]
	}
	return strings.Join(lines, "\n")
}

func clamp(val, lo, hi int) int {
	if val < lo {
		return lo
	}
	if val > hi {
		return hi
	}
	return val
}
