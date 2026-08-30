package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

// applyPersisted is the shared handler for the seven simple "save settings"
// persisted-message types. Each msg type carries only an err; the only
// thing that varies is the status label. Set m.settings.status to either
// failureLabel or successLabel and return the updated model.
func (m Model) applyPersisted(err error, failureLabel, successLabel string) Model {
	if err != nil {
		m.settings.status = failureLabel
	} else {
		m.settings.status = successLabel
	}
	return m
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.handleTickMsg(msg)

	case autoRefreshMsg:
		return m.handleAutoRefresh()

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
		return m.applyPersisted(msg.err, "save failed", "saved"), nil
	case dashboardProviderHideCostsPersistedMsg:
		return m.applyPersisted(msg.err, "hide_costs save failed", "hide_costs saved"), nil
	case dashboardViewPersistedMsg:
		return m.applyPersisted(msg.err, "view save failed", "view saved"), nil
	case dashboardUsageModePersistedMsg:
		return m.applyPersisted(msg.err, "usage mode save failed", "usage mode saved"), nil
	case dashboardWidgetSectionsPersistedMsg:
		return m.applyPersisted(msg.err, "section save failed", "sections saved"), nil
	case detailWidgetSectionsPersistedMsg:
		return m.applyPersisted(msg.err, "detail section save failed", "detail sections saved"), nil
	case dashboardHideSectionsWithNoDataPersistedMsg:
		return m.applyPersisted(msg.err, "empty-state save failed", "empty-state saved"), nil
	case themePersistedMsg:
		return m.applyPersisted(msg.err, "theme save failed", "theme saved"), nil
	case timeWindowPersistedMsg:
		return m.applyPersisted(msg.err, "time window save failed", "time window saved"), nil

	case providerLinkPersistedMsg:
		if msg.err != nil {
			m.settings.providerLinkPicker.status = "save failed: " + msg.err.Error()
			m.settings.status = "provider link save failed"
			return m, nil
		}
		m.settings.providerLinkPicker = providerLinkPickerState{}
		m.settings.status = fmt.Sprintf("mapped %s → %s", msg.source, msg.target)
		m = m.requestRefreshAll()
		return m, nil

	case providerLinkDeletedMsg:
		if msg.err != nil {
			m.settings.providerLinkPicker.status = "clear failed: " + msg.err.Error()
			m.settings.status = "provider link clear failed"
			return m, nil
		}
		m.settings.providerLinkPicker = providerLinkPickerState{}
		m.settings.status = fmt.Sprintf("cleared mapping for %s", msg.source)
		m = m.requestRefreshAll()
		return m, nil

	case availableBrowsersLoadedMsg:
		// Picker may have been dismissed (esc) before the scan finished —
		// or a fresh open replaced it for a different account. In either
		// case, drop this stale result on the floor.
		if !m.settings.browserPicker.active || m.settings.browserPicker.accountID != msg.AccountID {
			return m, nil
		}
		picker := &m.settings.browserPicker
		picker.loading = false
		if msg.Err != nil {
			picker.status = "could not enumerate browsers: " + msg.Err.Error()
			picker.browsers = nil
			return m, nil
		}
		picker.browsers = msg.Browsers
		picker.cursor = 0
		switch len(msg.Browsers) {
		case 0:
			picker.status = "no supported browsers found on this machine"
		case 1:
			picker.status = "found 1 browser — Enter to read its cookie"
		default:
			picker.status = fmt.Sprintf("found %d browsers — pick the one you log in with", len(msg.Browsers))
		}
		return m, nil

	case browserSessionConnectedMsg:
		if msg.Err != nil {
			m.settings.apiKeyStatus = "connect failed: " + msg.Err.Error()
			m.settings.status = "browser session connect failed for " + msg.AccountID
			return m, nil
		}
		m.ensureProviderTracking()
		providerID := providerForAccountID(msg.AccountID, m.accountProviders)
		if providerID != "" {
			authType := "browser_session"
			acct := core.AccountConfig{
				ID:       msg.AccountID,
				Provider: providerID,
				BrowserCookie: &core.BrowserCookieRef{
					Domain:        msg.Info.Domain,
					CookieName:    msg.Info.CookieName,
					SourceBrowser: msg.Info.SourceBrowser,
				},
			}
			if domain, cookieName, _ := browserCookieRefForProvider(providerID); domain != "" || cookieName != "" {
				acct.BrowserCookie.Domain = core.FirstNonEmpty(domain, acct.BrowserCookie.Domain)
				acct.BrowserCookie.CookieName = core.FirstNonEmpty(cookieName, acct.BrowserCookie.CookieName)
			}
			if isAPIKeyProvider(providerID) {
				authType = "api_key"
				acct.APIKeyEnv = resolvedAPIKeyEnvForProvider(providerID)
			}
			acct.Auth = authType
			if m.onAddAccount != nil {
				m.onAddAccount(acct)
			}
			m.accountProviders[msg.AccountID] = providerID
			if m.providerOrderIndex(msg.AccountID) < 0 {
				m.providerOrder = append(m.providerOrder, msg.AccountID)
				m.providerEnabled[msg.AccountID] = true
			}
		}
		m.settings.apiKeyStatus = fmt.Sprintf("connected via %s", msg.Info.SourceBrowser)
		m.settings.status = fmt.Sprintf("browser session connected for %s", msg.AccountID)
		// Trigger a fresh poll so the tile picks up the new auth path.
		m = m.requestRefreshAll()
		return m, nil

	case browserSessionDisconnectedMsg:
		if msg.Err != nil {
			m.settings.apiKeyStatus = "disconnect failed: " + msg.Err.Error()
			m.settings.status = "browser session disconnect failed for " + msg.AccountID
			return m, nil
		}
		m.settings.apiKeyStatus = "disconnected"
		m.settings.status = fmt.Sprintf("browser session removed for %s", msg.AccountID)
		m = m.requestRefreshAll()
		return m, nil

	case providerConsoleOpenedMsg:
		if msg.Err != nil {
			m.settings.apiKeyStatus = "open browser failed: " + msg.Err.Error()
			return m, nil
		}
		m.settings.apiKeyStatus = "opened browser — log in, then press Enter to read cookie"
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

func (m Model) handleAutoRefresh() (tea.Model, tea.Cmd) {
	if !m.refreshing {
		m = m.requestRefreshAll()
	}
	return m, autoRefreshCmd()
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
	if m.refreshing && m.pendingRefreshRequestID > 0 && msg.RequestID != m.pendingRefreshRequestID {
		if len(msg.Snapshots) > 0 && snapshotsReady(msg.Snapshots) {
			m.snapshots = msg.Snapshots
			m.lastDataUpdate = time.Now()
			m.invalidateRenderCaches()
			if msg.RequestID > m.lastSnapshotRequestID {
				m.lastSnapshotRequestID = msg.RequestID
			}
		}
		return m, m.restartTickIfNeeded()
	}
	if m.refreshing && m.hasData && !snapshotsReady(msg.Snapshots) {
		return m, nil
	}
	m.snapshots = msg.Snapshots
	m.refreshing = false
	m.refreshAll = false
	m.pendingRefreshRequestID = 0
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
		info := computeDisplayInfo(snap, dashboardWidget(snap.ProviderID), m.resolveHideCosts(snap))
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
		m.ensureProviderTracking()
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
	if m.showHelp {
		if msg.Action == tea.MouseActionPress {
			m.showHelp = false
			return m, nil
		}
		return m, nil
	}
	if m.settings.show {
		return m.handleSettingsMouse(msg)
	}
	if m.filter.active || m.analyticsFilter.active {
		return m, nil
	}
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		return m.handleMouseScroll(-m.mouseScrollStep(), msg.X), nil
	case tea.MouseButtonWheelDown:
		return m.handleMouseScroll(m.mouseScrollStep(), msg.X), nil
	case tea.MouseButtonLeft:
		return m.handleMouseLeftClick(msg)
	default:
		return m, nil
	}
}

func (m Model) handleMouseScroll(scroll, mouseX int) Model {
	if m.screen != screenDashboard {
		if m.screen == screenAnalytics {
			m.analyticsScrollY += scroll
			if m.analyticsScrollY < 0 {
				m.analyticsScrollY = 0
			}
		}
		return m
	}
	if m.mode == modeDetail {
		m.detailOffset += scroll
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
		return m
	}
	if m.mode == modeList {
		ids := m.filteredIDs()
		leftW := m.width / 3
		if leftW < minLeftWidth {
			leftW = minLeftWidth
		}
		if leftW > maxLeftWidth {
			leftW = maxLeftWidth
		}
		if leftW > m.width-34 {
			leftW = m.width - 34
		}
		if leftW < 10 {
			leftW = m.width / 2
		}

		if mouseX > 0 && mouseX <= leftW && len(ids) > 0 {
			if scroll < 0 && m.cursor > 0 {
				m.cursor--
				m.detailOffset = 0
			} else if scroll > 0 && m.cursor < len(ids)-1 {
				m.cursor++
				m.detailOffset = 0
			}
		} else {
			m.detailOffset += scroll
			if m.detailOffset < 0 {
				m.detailOffset = 0
			}
		}
	}
	return m
}

func (m Model) handleMouseLeftClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	headerLines := 2
	footerLines := 2

	// Check if click is on header (e.g. screen tabs)
	if msg.Y < headerLines {
		if msg.Y == 0 {
			screens := m.availableScreens()
			if len(screens) > 1 {
				bolt := PulseChar(
					accentBoldStyle.Render("⚡"),
					lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("⚡"),
					m.animFrame,
				)
				brandText := RenderGradientText("OpenUsage", m.animFrame)
				startX := lipgloss.Width(bolt) + 1 + lipgloss.Width(brandText) + 1

				for i, screen := range screens {
					label := screenLabelByTab[screen]
					tabStr := fmt.Sprintf("%d:%s", i+1, label)
					tabStyle := screenTabInactiveStyle
					if screen == m.screen {
						tabStyle = screenTabActiveStyle
					}
					rendered := tabStyle.Render(tabStr)
					tw := lipgloss.Width(rendered)
					if msg.X >= startX && msg.X < startX+tw {
						if m.screen != screen {
							m.screen = screen
							m.mode = modeList
							m.detailOffset = 0
							m.tileOffset = 0
						}
						return m, nil
					}
					startX += tw
				}
			}
		}
		return m, nil
	}

	// Check if click is on footer
	if msg.Y >= m.height-footerLines {
		return m, nil
	}

	if m.screen != screenDashboard {
		return m, nil
	}

	if m.mode == modeDetail {
		// Clicking at the top header area exits detail view
		if msg.Y <= headerLines+1 {
			m = m.exitDetailMode()
			return m, nil
		}
		return m, nil
	}

	ids := m.filteredIDs()
	if len(ids) == 0 {
		return m, nil
	}

	contentH := m.height - headerLines - footerLines
	if contentH < 1 {
		contentH = 1
	}
	clickContentY := msg.Y - headerLines
	if clickContentY < 0 {
		return m, nil
	}

	leftW := m.width / 3
	if leftW < minLeftWidth {
		leftW = minLeftWidth
	}
	if leftW > maxLeftWidth {
		leftW = maxLeftWidth
	}
	if leftW > m.width-34 {
		leftW = m.width - 34
	}
	if leftW < 10 {
		leftW = m.width / 2
	}

	if msg.X <= leftW {
		// Clicked in the left navigator list -> select item without entering full detail view
		clickedIdx := m.splitListHitTest(leftW, contentH, clickContentY, ids)
		if clickedIdx >= 0 && clickedIdx < len(ids) {
			m.cursor = clickedIdx
			m.detailOffset = 0
			m.detailTab = 0
			m.tileOffset = 0
			m.invalidateRenderCaches()
		}
	}
	return m, nil
}

func (m Model) splitListHitTest(w, h, clickContentY int, ids []string) int {
	if len(ids) == 0 {
		return -1
	}
	start, end := listVisibleWindow(m.snapshots, ids, m.cursor, h)
	itemHeight := 3

	currentY := 0
	if start > 0 {
		currentY++ // "▲ more" indicator line
	}

	for i := start; i < end; i++ {
		snap, ok := m.snapshots[ids[i]]
		if !ok {
			continue
		}
		pID := snap.ProviderID
		isFirstInGroup := (i == 0) || (i > 0 && m.snapshots[ids[i-1]].ProviderID != pID)
		if isFirstInGroup {
			if clickContentY == currentY {
				return i
			}
			currentY++
		}
		if clickContentY >= currentY && clickContentY < currentY+itemHeight {
			return i
		}
		currentY += itemHeight
	}

	return -1
}

func (m Model) handleSettingsMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		scroll := -m.mouseScrollStep()
		if m.settings.tab == settingsTabWidgetSections {
			m.settings.previewOffset += scroll
			if m.settings.previewOffset < 0 {
				m.settings.previewOffset = 0
			}
		} else {
			m.settings.bodyOffset += scroll
			if m.settings.bodyOffset < 0 {
				m.settings.bodyOffset = 0
			}
		}
		return m, nil
	case tea.MouseButtonWheelDown:
		scroll := m.mouseScrollStep()
		if m.settings.tab == settingsTabWidgetSections {
			m.settings.previewOffset += scroll
			if m.settings.previewOffset < 0 {
				m.settings.previewOffset = 0
			}
		} else {
			m.settings.bodyOffset += scroll
			if m.settings.bodyOffset < 0 {
				m.settings.bodyOffset = 0
			}
		}
		return m, nil
	case tea.MouseButtonLeft:
		return m.handleSettingsLeftClick(msg)
	default:
		return m, nil
	}
}

func (m Model) handleSettingsLeftClick(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width < 40 || m.height < 12 {
		return m, nil
	}

	contentW := m.width - 24
	if contentW < 68 {
		contentW = 68
	}
	if contentW > 92 {
		contentW = 92
	}
	panelInnerW := contentW - 4
	if panelInnerW < 40 {
		panelInnerW = 40
	}

	const modalBodyHeight = 20
	contentH := modalBodyHeight
	maxAllowed := m.height - 14
	if maxAllowed < 8 {
		maxAllowed = 8
	}
	if contentH > maxAllowed {
		contentH = maxAllowed
	}

	panelW := contentW + 2
	panelH := contentH + 8
	if m.settings.status != "" {
		panelH++
	}

	sideBySide := m.width >= contentW*2+12 && m.settings.tab == settingsTabWidgetSections
	totalModalW := panelW
	if sideBySide {
		totalModalW = contentW*2 + 6
	}

	modalStartX := (m.width - totalModalW) / 2
	modalStartY := (m.height - panelH) / 2

	// Check if clicked outside modal
	if msg.X < modalStartX || msg.X >= modalStartX+totalModalW || msg.Y < modalStartY || msg.Y >= modalStartY+panelH {
		m.settings.show = false
		m.settings.bodyOffset = 0
		m.settings.previewOffset = 0
		return m, nil
	}

	// Check if clicked on tabs line
	tabsY := modalStartY + 3
	if msg.Y == tabsY {
		tabsStartX := modalStartX + 3
		tabsWidth := panelInnerW
		n := len(settingsTabNames)
		if n > 0 && msg.X >= tabsStartX && msg.X < tabsStartX+tabsWidth {
			gap := 1
			cellW := (tabsWidth - gap*(n-1)) / n
			if cellW < 6 {
				cellW = 6
				gap = 0
				cellW = tabsWidth / n
			}
			relX := msg.X - tabsStartX
			stride := cellW + gap
			if stride > 0 {
				clickedTab := relX / stride
				if clickedTab >= 0 && clickedTab < n {
					m.settings.tab = settingsModalTab(clickedTab)
					m.settings.bodyOffset = 0
					m.settings.previewOffset = 0
					return m, nil
				}
			}
		}
	}

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
		case "p", "P", ",", "S":
			m.openSettingsModal()
			m.settings.tab = settingsTabProviders
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
		case "c":
			if m.screen == screenDashboard {
				if mdl, cmd, handled := m.toggleHideCostsOverride(); handled {
					return mdl, cmd
				}
			}
		case "w":
			return m.cycleTimeWindow()
		case "u", "U":
			if m.screen == screenDashboard {
				m.toggleUsageMode()
				return m, m.persistDashboardUsageModeCmd()
			}
		}
	}

	if m.screen == screenAnalytics {
		return m.handleAnalyticsKey(msg)
	}
	return m.handleDashboardTilesKey(msg)
}

// toggleHideCostsOverride cycles the per-account hide_costs override for the
// currently focused dashboard tile/row through auto (nil) → hide (true) →
// show (false) → auto. Returns handled=false when no tile is focused (so the
// keystroke can fall through to other handlers).
func (m Model) toggleHideCostsOverride() (Model, tea.Cmd, bool) {
	ids := m.filteredIDs()
	accountID := m.selectedTileID(ids)
	if accountID == "" {
		return m, nil, false
	}
	next := m.cycleHideCostsOverride(accountID)
	m.invalidateRenderCaches()
	return m, m.persistDashboardProviderHideCostsCmd(accountID, next), true
}

func (m Model) handleDashboardTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filter.active {
		return m.handleFilterKey(msg)
	}
	if m.mode == modeDetail {
		return m.handleDetailKey(msg)
	}
	return m.handleListKey(msg)
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
		return m.triggerRefreshFocused()
	case "R":
		return m.triggerRefreshAll()
	case "j", "down":
		m.analyticsScrollY++
	case "k", "up":
		if m.analyticsScrollY > 0 {
			m.analyticsScrollY--
		}
	case "pgdown":
		m.analyticsScrollY += 10
	case "pgup":
		if m.analyticsScrollY > 10 {
			m.analyticsScrollY -= 10
		} else {
			m.analyticsScrollY = 0
		}
	case "home", "g":
		m.analyticsScrollY = 0
	case "end", "G":
		m.analyticsScrollY = 9999
	}
	return m, nil
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
	case "shift+down", "shift+j":
		if len(ids) > 0 {
			m.cursor = clamp(m.cursor+pageStep, 0, len(ids)-1)
			m.detailOffset = 0
			m.detailTab = 0
			m.tileOffset = 0
		}
	case "shift+up", "shift+k":
		if len(ids) > 0 {
			m.cursor = clamp(m.cursor-pageStep, 0, len(ids)-1)
			m.detailOffset = 0
			m.detailTab = 0
			m.tileOffset = 0
		}
	case "pgdown", "ctrl+d":
		m.detailOffset += m.detailPageStep()
	case "pgup", "ctrl+u":
		m.detailOffset -= m.detailPageStep()
		if m.detailOffset < 0 {
			m.detailOffset = 0
		}
	case "home", "g":
		m.detailOffset = 0
	case "end", "G":
		m.detailOffset = 9999
	case "ctrl+o":
		if id := m.selectedTileID(ids); id != "" {
			m.expandedModelMixTiles[id] = !m.expandedModelMixTiles[id]
		}
	case "enter", "right", "l":
		m = m.enterDetailMode()
	case "/":
		m.filter.active = true
		m.filter.text = ""
	case "esc":
		if m.filter.text != "" {
			m.filter.text = ""
			m.cursor = 0
			m.tileOffset = 0
		} else {
			m.tileOffset = 0
		}
	case "r":
		return m.triggerRefreshFocused()
	case "R":
		return m.triggerRefreshAll()
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
		return m.triggerRefreshFocused()
	case "R":
		return m.triggerRefreshAll()
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
	sections := buildDetailSections(snap, dashboardWidget(snap.ProviderID), width, m.warnThreshold, m.critThreshold, m.timeWindow, m.resolveHideCosts(snap), m.viewNow())
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
		return m.triggerRefreshFocused()
	case "R":
		return m.triggerRefreshAll()
	}
	return m, nil
}
