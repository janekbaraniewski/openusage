package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTickMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.invalidateRenderCaches()
		return m, nil

	case DaemonStatusMsg:
		m.daemon.status = msg.Status
		m.daemon.message = msg.Message
		if msg.Status == DaemonRunning {
			m.daemon.installing = false
		}
		return m, m.restartTickIfNeeded()

	case AppUpdateMsg:
		m.daemon.appUpdateCurrent = strings.TrimSpace(msg.CurrentVersion)
		m.daemon.appUpdateLatest = strings.TrimSpace(msg.LatestVersion)
		m.daemon.appUpdateHint = strings.TrimSpace(msg.UpgradeHint)
		return m, nil

	case daemonInstallResultMsg:
		return m.handleDaemonInstallResultMsg(msg)

	case SnapshotsMsg:
		return m.handleSnapshotsMsg(msg)

	case dashboardPrefsPersistedMsg:
		if msg.err != nil {
			m.settings.status = "save failed"
		} else {
			m.settings.status = "saved"
		}
		return m, nil

	case dashboardViewPersistedMsg:
		if msg.err != nil {
			m.settings.status = "view save failed"
		} else {
			m.settings.status = "view saved"
		}
		return m, nil

	case dashboardWidgetSectionsPersistedMsg:
		if msg.err != nil {
			m.settings.status = "section save failed"
		} else {
			m.settings.status = "sections saved"
		}
		return m, nil

	case detailWidgetSectionsPersistedMsg:
		if msg.err != nil {
			m.settings.status = "detail section save failed"
		} else {
			m.settings.status = "detail sections saved"
		}
		return m, nil

	case dashboardHideSectionsWithNoDataPersistedMsg:
		if msg.err != nil {
			m.settings.status = "empty-state save failed"
		} else {
			m.settings.status = "empty-state saved"
		}
		return m, nil

	case themePersistedMsg:
		if msg.err != nil {
			m.settings.status = "theme save failed"
		} else {
			m.settings.status = "theme saved"
		}
		return m, nil

	case timeWindowPersistedMsg:
		if msg.err != nil {
			m.settings.status = "time window save failed"
		} else {
			m.settings.status = "time window saved"
		}
		return m, nil

	case validateKeyResultMsg:
		return m.handleValidateKeyResultMsg(msg)

	case credentialSavedMsg:
		return m.handleCredentialSavedMsg(msg)

	case credentialDeletedMsg:
		if msg.Err != nil {
			m.settings.status = "delete failed"
		} else {
			m.settings.status = "key deleted"
		}
		return m, nil

	case integrationInstallResultMsg:
		return m.handleIntegrationInstallResultMsg(msg)

	case tea.KeyMsg:
		m.lastInteraction = time.Now()
		cmd := m.restartTickIfNeeded()
		if !m.hasData {
			mdl, keyCmd := m.handleSplashKey(msg)
			return mdl, tea.Batch(cmd, keyCmd)
		}
		mdl, keyCmd := m.handleKey(msg)
		return mdl, tea.Batch(cmd, keyCmd)
	case tea.MouseMsg:
		m.lastInteraction = time.Now()
		cmd := m.restartTickIfNeeded()
		mdl, mouseCmd := m.handleMouse(msg)
		return mdl, tea.Batch(cmd, mouseCmd)
	}
	return m, nil
}

func (m Model) handleTickMsg(_ tickMsg) (tea.Model, tea.Cmd) {
	m.animFrame++
	interval := m.nextTickInterval()
	if interval == 0 {
		m.tickRunning = false
		return m, nil
	}
	return m, scheduleTickCmd(interval)
}

func (m Model) handleDaemonInstallResultMsg(msg daemonInstallResultMsg) (tea.Model, tea.Cmd) {
	m.daemon.installing = false
	if msg.err != nil {
		m.daemon.status = DaemonError
		m.daemon.message = msg.err.Error()
	} else {
		m.daemon.installDone = true
		m.daemon.status = DaemonStarting
	}
	return m, nil
}

func (m Model) handleSnapshotsMsg(msg SnapshotsMsg) (tea.Model, tea.Cmd) {
	msgWindow := msg.TimeWindow
	if msgWindow == "" {
		msgWindow = core.TimeWindow30d
	}
	if msgWindow != m.timeWindow {
		return m, nil
	}
	if msg.RequestID > 0 && msg.RequestID < m.lastSnapshotRequestID {
		return m, nil
	}
	if m.refreshing && m.hasData && !snapshotsReady(msg.Snapshots) {
		return m, nil
	}
	m.snapshots = msg.Snapshots
	m.refreshing = false
	m.lastDataUpdate = time.Now()
	m.invalidateRenderCaches()
	if msg.RequestID > m.lastSnapshotRequestID {
		m.lastSnapshotRequestID = msg.RequestID
	}
	if len(msg.Snapshots) > 0 || snapshotsReady(msg.Snapshots) {
		m.hasData = true
		m.daemon.status = DaemonRunning
	}
	for id, snap := range m.snapshots {
		info := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID))
		if info.reason != "" {
			snap.EnsureMaps()
			snap.Diagnostics["display_branch"] = info.reason
			m.snapshots[id] = snap
		}
	}
	m.ensureSnapshotProvidersKnown()
	m.rebuildSortedIDs()
	return m, m.restartTickIfNeeded()
}

func (m Model) handleValidateKeyResultMsg(msg validateKeyResultMsg) (tea.Model, tea.Cmd) {
	if msg.Valid {
		m.settings.apiKeyStatus = "valid ✓ — saving..."
		return m, m.saveCredentialCmd(msg.AccountID, m.settings.apiKeyInput)
	}
	m.settings.apiKeyStatus = "invalid ✗"
	if msg.Error != "" {
		errMsg := msg.Error
		if len(errMsg) > 40 {
			errMsg = errMsg[:37] + "..."
		}
		m.settings.apiKeyStatus = "invalid: " + errMsg
	}
	return m, nil
}

func (m Model) handleCredentialSavedMsg(msg credentialSavedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		m.settings.apiKeyStatus = "save failed"
	} else {
		m.settings.apiKeyStatus = "saved ✓"
		apiKey := m.settings.apiKeyInput
		m.settings.apiKeyEditing = false
		m.settings.apiKeyInput = ""
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
		if m.providerOrderIndex(msg.AccountID) < 0 {
			m.providerOrder = append(m.providerOrder, msg.AccountID)
			m.providerEnabled[msg.AccountID] = true
		}
		m.refreshing = true
	}
	return m, nil
}

func (m Model) handleIntegrationInstallResultMsg(msg integrationInstallResultMsg) (tea.Model, tea.Cmd) {
	m.settings.integrationStatus = msg.Statuses
	if msg.Err != nil {
		errMsg := msg.Err.Error()
		if len(errMsg) > 80 {
			errMsg = errMsg[:77] + "..."
		}
		m.settings.status = "integration install failed: " + errMsg
	} else {
		m.settings.status = "integration installed"
	}
	return m, nil
}

func (m Model) handleSplashKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "enter":
		if (m.daemon.status == DaemonNotInstalled || m.daemon.status == DaemonOutdated) && !m.daemon.installing {
			m.daemon.installing = true
			m.daemon.message = "Setting up background helper..."
			return m, m.installDaemonCmd()
		}
	}
	return m, nil
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.settings.show {
		return m.handleSettingsMouse(msg)
	}
	if m.showHelp || m.filter.active || m.analyticsFilter.active {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft {
		return m.handleMouseClick(msg)
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
		// Detail view uses plain content scrolling only.
		m.detailOffset += scroll
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
		return m, nil
	}
	if m.mode == modeList && (m.shouldUseWidgetScroll() || m.shouldUsePanelScroll()) {
		m.tileOffset += scroll
		if m.tileOffset < 0 {
			m.tileOffset = 0
		}
		return m, nil
	}
	if m.mode == modeList && m.activeDashboardView() == dashboardViewSplit {
		step := 1
		if scroll < 0 {
			step = -1
		}
		next := m.cursor + step
		ids := m.filteredIDs()
		if next < 0 {
			next = 0
		}
		if next >= len(ids) {
			next = len(ids) - 1
		}
		if next < 0 {
			next = 0
		}
		m.cursor = next
	}
	return m, nil
}

func (m Model) handleSettingsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress || m.settings.tab != settingsTabWidgetSections {
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

	m.settings.previewOffset += scroll
	if m.settings.previewOffset < 0 {
		m.settings.previewOffset = 0
	}
	return m, nil
}

func (m Model) handleMouseClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.screen != screenDashboard || m.mode != modeList {
		return m, nil
	}

	ids := m.filteredIDs()
	if len(ids) == 0 {
		return m, nil
	}
	view := m.activeDashboardView()
	if view != dashboardViewGrid && view != dashboardViewStacked {
		return m, nil
	}

	contentH := m.height - 3
	if contentH < 5 {
		contentH = 5
	}
	cols, tileW, tileMaxH := m.tileGrid(m.width, contentH, len(ids))
	if view == dashboardViewStacked {
		cols = 1
	}

	clickY := msg.Y - 2
	clickX := msg.X - 1
	if clickX < 0 || clickY < 0 {
		return m, nil
	}

	cellW := tileW + tileBorderH + tileGapH
	if cellW <= 0 {
		return m, nil
	}
	col := clickX / cellW
	if col >= cols {
		return m, nil
	}

	rowH := tileMaxH + tileBorderV
	if tileMaxH <= 0 {
		rowH = contentH
		if len(ids) > 1 {
			rowH = contentH / len(ids)
		}
		if rowH < tileMinHeight+tileBorderV {
			rowH = tileMinHeight + tileBorderV
		}
	}
	rowCell := rowH + tileGapV
	if rowCell <= 0 {
		return m, nil
	}

	cursorRow := m.cursor / cols
	totalRows := (len(ids) + cols - 1) / cols
	rowOffsets := make([]int, totalRows)
	acc := 0
	for r := 0; r < totalRows; r++ {
		rowOffsets[r] = acc
		acc += rowH
		if r < totalRows-1 {
			acc += tileGapV
		}
	}

	rowScrollOffset := 0
	if cols == 1 {
		rowScrollOffset = m.tileOffset
	}
	scrollLine := 0
	if cursorRow >= 0 && cursorRow < totalRows {
		scrollLine = rowOffsets[cursorRow] + rowScrollOffset
	}
	if scrollLine > acc-contentH {
		scrollLine = acc - contentH
	}
	if scrollLine < 0 {
		scrollLine = 0
	}

	absY := clickY + scrollLine
	row := -1
	for r := 0; r < totalRows; r++ {
		if absY >= rowOffsets[r] && absY < rowOffsets[r]+rowH {
			row = r
			break
		}
	}
	if row < 0 {
		return m, nil
	}

	idx := row*cols + col
	if idx < 0 || idx >= len(ids) {
		return m, nil
	}

	m.cursor = idx
	m.tileOffset = 0
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "?" && !m.filter.active && !m.analyticsFilter.active && !m.settings.show {
		m.showHelp = !m.showHelp
		return m, nil
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	if m.settings.show {
		return m.handleSettingsModalKey(msg)
	}

	if !m.filter.active && !m.analyticsFilter.active {
		if m.screen == screenDashboard && m.mode == modeDetail {
			switch msg.String() {
			case "tab", "shift+tab", "left", "h", "right", "l":
				return m.handleDetailKey(msg)
			}
		}
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
			m.invalidateRenderCaches()
			return m, m.persistThemeCmd(CycleTheme())
		case "w":
			return m.cycleTimeWindow()
		case "v":
			if m.screen == screenDashboard {
				m.setDashboardView(m.nextDashboardView(1))
				return m, m.persistDashboardViewCmd()
			}
		case "V":
			if m.screen == screenDashboard {
				m.setDashboardView(m.nextDashboardView(-1))
				return m, m.persistDashboardViewCmd()
			}
		}
	}

	if m.screen == screenAnalytics {
		return m.handleAnalyticsKey(msg)
	}
	return m.handleDashboardTilesKey(msg)
}

func (m Model) handleDashboardTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter.active {
		return m.handleFilterKey(msg)
	}
	if m.mode == modeDetail {
		return m.handleDetailKey(msg)
	}
	if m.activeDashboardView() == dashboardViewSplit {
		return m.handleListKey(msg)
	}
	return m.handleTilesKey(msg)
}

func (m Model) handleAnalyticsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.analyticsFilter.active {
		return m.handleAnalyticsFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "s":
		m.analyticsSortBy = (m.analyticsSortBy + 1) % analyticsSortCount
		m.invalidateAnalyticsCache()
	case "/":
		m.analyticsFilter.active = true
		m.analyticsFilter.text = ""
	case "esc":
		if m.analyticsFilter.text != "" {
			m.analyticsFilter.text = ""
			m.invalidateAnalyticsCache()
		} else {
			m.analyticsScrollY = 0
		}
	case "r":
		m = m.requestRefresh()

	// Sub-tab navigation
	case "[":
		if m.analyticsTab > 0 {
			m.analyticsTab--
			m.analyticsScrollY = 0
			m.invalidateAnalyticsCache()
		}
	case "]":
		if m.analyticsTab < analyticsTabCount-1 {
			m.analyticsTab++
			m.analyticsScrollY = 0
			m.invalidateAnalyticsCache()
		}
	case "1":
		m.analyticsTab = analyticsTabOverview
		m.analyticsScrollY = 0
		m.invalidateAnalyticsCache()
	case "2":
		m.analyticsTab = analyticsTabModels
		m.analyticsScrollY = 0
		m.invalidateAnalyticsCache()
	case "3":
		m.analyticsTab = analyticsTabSpend
		m.analyticsScrollY = 0
		m.invalidateAnalyticsCache()
	case "4":
		m.analyticsTab = analyticsTabActivity
		m.analyticsScrollY = 0
		m.invalidateAnalyticsCache()

	// Model cursor navigation (Models tab)
	case "j", "down":
		if m.analyticsTab == analyticsTabModels {
			m.analyticsModelCursor++
			m.clampAnalyticsModelCursor()
			m.invalidateAnalyticsCache()
		} else {
			m.analyticsScrollY++
		}
	case "k", "up":
		if m.analyticsTab == analyticsTabModels {
			if m.analyticsModelCursor > 0 {
				m.analyticsModelCursor--
				m.invalidateAnalyticsCache()
			}
		} else if m.analyticsScrollY > 0 {
			m.analyticsScrollY--
		}
	case "enter", " ":
		if m.analyticsTab == analyticsTabModels {
			m.toggleAnalyticsModelExpand()
			m.invalidateAnalyticsCache()
		}
	case "pgdown":
		if m.analyticsTab == analyticsTabModels {
			m.analyticsModelCursor += 5
			m.clampAnalyticsModelCursor()
			m.invalidateAnalyticsCache()
		} else {
			m.analyticsScrollY += 10
		}
	case "pgup":
		if m.analyticsTab == analyticsTabModels {
			m.analyticsModelCursor -= 5
			if m.analyticsModelCursor < 0 {
				m.analyticsModelCursor = 0
			}
			m.invalidateAnalyticsCache()
		} else if m.analyticsScrollY > 10 {
			m.analyticsScrollY -= 10
		} else {
			m.analyticsScrollY = 0
		}
	case "home", "g":
		if m.analyticsTab == analyticsTabModels {
			m.analyticsModelCursor = 0
			m.invalidateAnalyticsCache()
		} else {
			m.analyticsScrollY = 0
		}
	case "end", "G":
		if m.analyticsTab == analyticsTabModels {
			m.analyticsModelCursor = 9999 // will be clamped
			m.clampAnalyticsModelCursor()
			m.invalidateAnalyticsCache()
		}
	}
	return m, nil
}

func (m *Model) clampAnalyticsModelCursor() {
	data := extractCostData(m.visibleSnapshots(), m.analyticsFilter.text, m.timeWindow)
	models := filterTokenModels(data.models)
	max := len(models) - 1
	if max < 0 {
		max = 0
	}
	if m.analyticsModelCursor > max {
		m.analyticsModelCursor = max
	}
}

func (m *Model) toggleAnalyticsModelExpand() {
	data := extractCostData(m.visibleSnapshots(), m.analyticsFilter.text, m.timeWindow)
	sortModels(data.models, m.analyticsSortBy)
	models := filterTokenModels(data.models)
	if m.analyticsModelCursor >= 0 && m.analyticsModelCursor < len(models) {
		name := models[m.analyticsModelCursor].name
		m.analyticsModelExpand[name] = !m.analyticsModelExpand[name]
	}
}

func (m Model) handleAnalyticsFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.analyticsFilter.active = false
	case "esc":
		m.analyticsFilter.active = false
		if m.analyticsFilter.text != "" {
			m.analyticsFilter.text = ""
			m.invalidateAnalyticsCache()
		}
	case "backspace":
		if len(m.analyticsFilter.text) > 0 {
			m.analyticsFilter.text = m.analyticsFilter.text[:len(m.analyticsFilter.text)-1]
			m.invalidateAnalyticsCache()
		}
	default:
		if len(msg.String()) == 1 {
			m.analyticsFilter.text += msg.String()
			m.invalidateAnalyticsCache()
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
	pageStep := m.listPageStep()
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
	case "pgdown", "ctrl+d":
		if len(ids) > 0 {
			m.cursor = clamp(m.cursor+pageStep, 0, len(ids)-1)
		}
	case "pgup", "ctrl+u":
		if len(ids) > 0 {
			m.cursor = clamp(m.cursor-pageStep, 0, len(ids)-1)
		}
	case "enter", "right", "l":
		m = m.enterDetailMode()
	case "/":
		m.filter.active = true
		m.filter.text = ""
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}

func (m Model) handleDetailKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc", "backspace":
		m = m.exitDetailMode()
	case "shift+tab", "left", "h":
		m = m.navigateDetailSection(-1)
	case "tab", "right", "l":
		m = m.navigateDetailSection(1)
	case "up", "k":
		if m.detailOffset > 0 {
			m.detailOffset--
		}
	case "down", "j":
		m.detailOffset++
	case "g":
		m.detailOffset = 0
	case "G":
		m.detailOffset = 9999
	case "[":
		if m.detailTab > 0 {
			m.detailTab--
			m.detailOffset = 0
		}
	case "]":
		m.detailTab++
		m.detailOffset = 0
	case "pgdown", "ctrl+d":
		m.detailOffset += m.detailPageStep()
	case "pgup", "ctrl+u":
		m.detailOffset -= m.detailPageStep()
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}

func (m Model) navigateDetailSection(step int) Model {
	starts := m.detailSectionStarts()
	if len(starts) == 0 {
		return m
	}

	current := max(0, m.detailOffset)
	if step > 0 {
		for _, start := range starts {
			if start > current {
				m.detailOffset = start
				return m
			}
		}
		m.detailOffset = starts[len(starts)-1]
		return m
	}

	prev := 0
	for _, start := range starts {
		if start >= current {
			break
		}
		prev = start
	}
	m.detailOffset = prev
	return m
}

func (m Model) detailSectionStarts() []int {
	ids := m.filteredIDs()
	if len(ids) == 0 || m.cursor < 0 || m.cursor >= len(ids) {
		return nil
	}

	snap, ok := m.snapshots[ids[m.cursor]]
	if !ok {
		return nil
	}

	width := m.width - 2
	if width < 30 {
		width = 30
	}
	sections := buildDetailSections(snap, dashboardWidget(snap.ProviderID), width, m.warnThreshold, m.critThreshold, m.timeWindow)
	if len(sections) == 0 {
		return nil
	}

	line := 3 // compact detail header lines
	starts := make([]int, 0, len(sections))
	for _, sec := range sections {
		if len(sec.lines) == 0 {
			continue
		}
		line++ // blank line before each card
		starts = append(starts, line)
		line += len(sec.lines) + 2 // top border + body + bottom border
	}
	return starts
}

func (m Model) detailPageStep() int {
	step := m.height / 2
	if step < 3 {
		step = 3
	}
	return step
}

func (m Model) handleFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filter.active = false
		m.cursor = 0
		m.tileOffset = 0
	case "esc":
		m.filter.text = ""
		m.filter.active = false
		m.cursor = 0
		m.tileOffset = 0
	case "backspace":
		if len(m.filter.text) > 0 {
			m.filter.text = m.filter.text[:len(m.filter.text)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.filter.text += msg.String()
		}
	}
	return m, nil
}

func (m Model) handleTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	ids := m.filteredIDs()
	cols := m.tileCols()
	scrollModeWidget := m.shouldUseWidgetScroll()
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
		if scrollModeWidget {
			m.tileOffset += m.widgetScrollStep()
		} else {
			m.tileOffset += m.tileScrollStep()
		}
	case "pgup", "ctrl+u":
		if scrollModeWidget {
			m.tileOffset -= m.widgetScrollStep()
		} else {
			m.tileOffset -= m.tileScrollStep()
		}
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
		m.tileOffset = 9999
	case "enter":
		m = m.enterDetailMode()
	case "/":
		m.filter.active = true
		m.filter.text = ""
	case "esc":
		if m.filter.text != "" {
			m.filter.text = ""
			m.cursor = 0
			m.tileOffset = 0
		}
	case "r":
		m = m.requestRefresh()
	}
	return m, nil
}
