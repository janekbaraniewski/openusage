package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/integrations"
	"github.com/samber/lo"
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

type SnapshotsMsg struct {
	Snapshots  map[string]core.UsageSnapshot
	TimeWindow core.TimeWindow
	RequestID  uint64
}

type DaemonStatus string

const (
	DaemonConnecting   DaemonStatus = "connecting"
	DaemonNotInstalled DaemonStatus = "not_installed"
	DaemonStarting     DaemonStatus = "starting"
	DaemonRunning      DaemonStatus = "running"
	DaemonOutdated     DaemonStatus = "outdated"
	DaemonError        DaemonStatus = "error"
)

type DaemonStatusMsg struct {
	Status      DaemonStatus
	Message     string
	InstallHint string
}

type AppUpdateMsg struct {
	CurrentVersion string
	LatestVersion  string
	UpgradeHint    string
}

type daemonInstallResultMsg struct {
	err error
}

// filterState is a reusable text filter for list views.
type filterState struct {
	text   string
	active bool
}

// daemonState tracks daemon connection and app update status.
type daemonState struct {
	status      DaemonStatus
	message     string
	installing  bool
	installDone bool // true after a successful install in this session

	appUpdateCurrent string
	appUpdateLatest  string
	appUpdateHint    string
}

// settingsState tracks the settings modal state.
type settingsState struct {
	show              bool
	tab               settingsModalTab
	cursor            int
	bodyOffset        int
	themeCursor       int
	viewCursor        int
	sectionRowCursor  int
	previewOffset     int
	status            string
	integrationStatus []integrations.Status

	apiKeyEditing       bool
	apiKeyInput         string
	apiKeyEditAccountID string
	apiKeyStatus        string // "validating...", "valid ✓", "invalid ✗", etc.
}

type Services interface {
	SaveTheme(themeName string) error
	SaveDashboardProviders(providers []config.DashboardProviderConfig) error
	SaveDashboardView(view string) error
	SaveDashboardWidgetSections(sections []config.DashboardWidgetSection) error
	SaveDashboardHideSectionsWithNoData(hide bool) error
	SaveTimeWindow(window string) error
	ValidateAPIKey(accountID, providerID, apiKey string) (bool, string)
	SaveCredential(accountID, apiKey string) error
	DeleteCredential(accountID string) error
	InstallIntegration(id integrations.ID) ([]integrations.Status, error)
}

type Model struct {
	snapshots map[string]core.UsageSnapshot
	sortedIDs []string
	cursor    int
	mode      viewMode
	filter    filterState
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

	dashboardView dashboardViewMode

	analyticsFilter filterState
	analyticsSortBy int // 0=cost↓, 1=name↑, 2=tokens↓

	animFrame  int // monotonically increasing frame counter
	refreshing bool
	hasData    bool

	experimentalAnalytics bool // when false, only the Dashboard screen is available

	daemon daemonState

	providerOrder    []string
	providerEnabled  map[string]bool
	accountProviders map[string]string

	settings               settingsState
	widgetSections         []config.DashboardWidgetSection
	hideSectionsWithNoData bool

	timeWindow            core.TimeWindow
	lastSnapshotRequestID uint64

	services           Services
	onAddAccount       func(core.AccountConfig)
	onRefresh          func(core.TimeWindow)
	onInstallDaemon    func() error
	onTimeWindowChange func(core.TimeWindow)
}

func NewModel(
	warnThresh, critThresh float64,
	experimentalAnalytics bool,
	dashboardCfg config.DashboardConfig,
	accounts []core.AccountConfig,
	timeWindow core.TimeWindow,
) Model {
	model := Model{
		snapshots:             make(map[string]core.UsageSnapshot),
		warnThreshold:         warnThresh,
		critThreshold:         critThresh,
		experimentalAnalytics: experimentalAnalytics,
		providerEnabled:       make(map[string]bool),
		accountProviders:      make(map[string]string),
		expandedModelMixTiles: make(map[string]bool),
		daemon:                daemonState{status: DaemonConnecting},
		timeWindow:            timeWindow,
	}

	model.applyDashboardConfig(dashboardCfg, accounts)
	return model
}

func (m *Model) SetOnInstallDaemon(fn func() error) {
	m.onInstallDaemon = fn
}

func (m *Model) SetServices(services Services) {
	m.services = services
}

// SetOnAddAccount sets a callback invoked when a new provider account is added via the API Keys tab.
func (m *Model) SetOnAddAccount(fn func(core.AccountConfig)) {
	m.onAddAccount = fn
}

func (m *Model) SetOnRefresh(fn func(core.TimeWindow)) {
	m.onRefresh = fn
}

func (m *Model) SetOnTimeWindowChange(fn func(core.TimeWindow)) {
	m.onTimeWindowChange = fn
}

type themePersistedMsg struct {
	err error
}
type dashboardPrefsPersistedMsg struct {
	err error
}
type dashboardViewPersistedMsg struct {
	err error
}
type dashboardWidgetSectionsPersistedMsg struct {
	err error
}
type dashboardHideSectionsWithNoDataPersistedMsg struct {
	err error
}
type timeWindowPersistedMsg struct {
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

func (m Model) Init() tea.Cmd { return tickCmd() }

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

func (m Model) widgetScrollStep() int {
	step := m.height / 8
	if step < 2 {
		step = 2
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

func (m Model) listPageStep() int {
	step := m.height / 6
	if step < 3 {
		step = 3
	}
	return step
}

func (m Model) shouldUseWidgetScroll() bool {
	if m.screen != screenDashboard || m.mode != modeList {
		return false
	}
	switch m.activeDashboardView() {
	case dashboardViewTabs, dashboardViewCompare, dashboardViewSplit:
		return true
	case dashboardViewGrid:
		return m.tileCols() > 1
	default:
		return false
	}
}

func (m Model) shouldUsePanelScroll() bool {
	if m.screen != screenDashboard || m.mode != modeList {
		return false
	}
	if m.shouldUseWidgetScroll() {
		return false
	}
	if m.activeDashboardView() == dashboardViewSplit {
		return false
	}
	return m.tileCols() == 1
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
	if m.settings.show {
		return m.renderSettingsModalOverlay()
	}
	return view
}

func (m Model) renderDashboardContent(w, contentH int) string {
	if m.mode == modeDetail {
		return m.renderDetailPanel(w, contentH)
	}
	switch m.activeDashboardView() {
	case dashboardViewTabs:
		return m.renderTilesTabs(w, contentH)
	case dashboardViewSplit:
		return m.renderSplitPanes(w, contentH)
	case dashboardViewCompare:
		return m.renderComparePanes(w, contentH)
	case dashboardViewStacked:
		return m.renderTilesSingleColumn(w, contentH)
	default:
		return m.renderTiles(w, contentH)
	}
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

	if m.settings.show {
		info = m.settingsModalInfo()
	} else {
		switch m.screen {
		case screenAnalytics:
			info = dimStyle.Render("spend analysis")
			if m.analyticsFilter.text != "" {
				info += " (filtered)"
			}
		default:
			info = fmt.Sprintf("⊞ %d providers", len(ids))
			if m.filter.text != "" {
				info += " (filtered)"
			}
			info += " · " + m.dashboardViewStatusLabel()
		}
	}
	if !m.settings.show {
		twLabel := m.timeWindow.Label()
		info += " · " + twLabel
	}
	if !m.settings.show && len(unmappedProviders) > 0 {
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
	statusLine := m.renderFooterStatusLine(w)
	return sep + "\n" + statusLine
}

func (m Model) renderFooterStatusLine(w int) string {
	searchStyle := lipgloss.NewStyle().Foreground(colorSapphire)

	switch {
	case m.settings.show:
		if m.settings.status != "" {
			return " " + dimStyle.Render(m.settings.status)
		}
		return " " + helpStyle.Render("? help")
	case m.screen == screenAnalytics:
		if m.analyticsFilter.active {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.analyticsFilter.text+cursor)
		}
		if m.analyticsFilter.text != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.analyticsFilter.text)
		}
	default:
		if m.filter.active {
			cursor := PulseChar("█", "▌", m.animFrame)
			return " " + dimStyle.Render("search: ") + searchStyle.Render(m.filter.text+cursor)
		}
		if m.filter.text != "" {
			return " " + dimStyle.Render("filter: ") + searchStyle.Render(m.filter.text)
		}
		if m.activeDashboardView() == dashboardViewTabs && m.mode == modeList {
			return " " + dimStyle.Render("tabs view · \u2190/\u2192 switch tab · PgUp/PgDn scroll widget · Enter detail")
		}
		if m.activeDashboardView() == dashboardViewSplit && m.mode == modeList {
			return " " + dimStyle.Render("split view · \u2191/\u2193 select provider · PgUp/PgDn scroll pane · Enter detail")
		}
		if m.activeDashboardView() == dashboardViewCompare && m.mode == modeList {
			return " " + dimStyle.Render("compare view · \u2190/\u2192 switch provider · PgUp/PgDn scroll active pane")
		}
		if m.mode == modeList && m.shouldUseWidgetScroll() && m.tileOffset > 0 {
			return " " + dimStyle.Render("widget scroll active · PgUp/PgDn · Ctrl+U/Ctrl+D")
		}
		if m.mode == modeList && m.shouldUsePanelScroll() && m.tileOffset > 0 {
			return " " + dimStyle.Render("panel scroll active · PgUp/PgDn · Home/End")
		}
	}

	if m.hasAppUpdateNotice() {
		msg := "Update available: " + m.daemon.appUpdateCurrent + " -> " + m.daemon.appUpdateLatest
		if action := m.appUpdateAction(); action != "" {
			msg += " · " + action
		}
		if w > 2 {
			msg = truncateToWidth(msg, w-2)
		}
		return " " + lipgloss.NewStyle().Foreground(colorYellow).Render(msg)
	}

	return " " + helpStyle.Render("? help")
}

func (m Model) hasAppUpdateNotice() bool {
	return strings.TrimSpace(m.daemon.appUpdateCurrent) != "" && strings.TrimSpace(m.daemon.appUpdateLatest) != ""
}

func (m Model) appUpdateHeadline() string {
	if !m.hasAppUpdateNotice() {
		return ""
	}
	return "OpenUsage update available: " + m.daemon.appUpdateCurrent + " -> " + m.daemon.appUpdateLatest
}

func (m Model) appUpdateAction() string {
	hint := strings.TrimSpace(m.daemon.appUpdateHint)
	if hint == "" {
		return ""
	}
	return "Run: " + hint
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
	out := padToSize(content, w, h)
	if len(ids) > visibleItems && h > 0 {
		rendered := strings.Split(out, "\n")
		if len(rendered) > 0 {
			rendered[len(rendered)-1] = renderVerticalScrollBarLine(w, scrollStart, visibleItems, len(ids))
			out = strings.Join(rendered, "\n")
		}
	}
	return out
}

func (m Model) renderSplitPanes(w, h int) string {
	if w < 70 {
		return m.renderTilesTabs(w, h)
	}

	leftW := w / 3
	if leftW < minLeftWidth {
		leftW = minLeftWidth
	}
	if leftW > maxLeftWidth {
		leftW = maxLeftWidth
	}
	if leftW > w-34 {
		leftW = w - 34
	}
	if leftW < minLeftWidth || w-leftW-1 < 30 {
		return m.renderTilesTabs(w, h)
	}

	left := m.renderList(leftW, h)
	rightW := w - leftW - 1
	right := m.renderWidgetPanelByIndex(m.cursor, rightW, h, m.tileOffset, true)
	sep := renderVerticalSep(h)

	return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
}

func (m Model) renderComparePanes(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		return m.renderTiles(w, h)
	}
	if len(ids) == 1 || w < 72 {
		return m.renderWidgetPanelByIndex(m.cursor, w, h, m.tileOffset, true)
	}

	gapW := tileGapH
	colW := (w - gapW) / 2
	if colW < 30 {
		return m.renderWidgetPanelByIndex(m.cursor, w, h, m.tileOffset, true)
	}

	primary := clamp(m.cursor, 0, len(ids)-1)
	secondary := primary + 1
	if secondary >= len(ids) {
		secondary = primary - 1
	}
	if secondary < 0 {
		secondary = primary
	}

	left := m.renderWidgetPanelByIndex(primary, colW, h, m.tileOffset, true)
	right := m.renderWidgetPanelByIndex(secondary, colW, h, 0, false)

	row := lipgloss.JoinHorizontal(lipgloss.Top, left, strings.Repeat(" ", gapW), right)
	return padToSize(row, w, h)
}

func (m Model) renderWidgetPanelByIndex(index, w, h, bodyOffset int, selected bool) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || index < 0 || index >= len(ids) {
		return padToSize("", w, h)
	}

	id := ids[index]
	snap := m.snapshots[id]
	modelMixExpanded := index == m.cursor && m.expandedModelMixTiles[id]

	tileW := w - 2 - tileBorderH
	if tileW < tileMinWidth {
		tileW = tileMinWidth
	}
	contentH := h - tileBorderV
	if contentH < tileMinHeight {
		contentH = tileMinHeight
	}

	rendered := m.renderTile(snap, selected, modelMixExpanded, tileW, contentH, bodyOffset)
	return normalizeAnsiBlock(rendered, w, h)
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
	reason       string  // Decision branch name for diagnostics (e.g. "usage_five_hour", "spend_limit")
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
		info.reason = "status_error"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		if msg == "" {
			msg = "Error"
		}
		info.summary = msg
		core.Tracef("[display] %s: branch=status_error", snap.ProviderID)
		return info
	case core.StatusAuth:
		info.tagEmoji = "🔑"
		info.tagLabel = "Auth"
		info.reason = "status_auth"
		info.summary = "Authentication required"
		core.Tracef("[display] %s: branch=status_auth", snap.ProviderID)
		return info
	case core.StatusUnsupported:
		info.tagEmoji = "◇"
		info.tagLabel = "N/A"
		info.reason = "status_unsupported"
		info.summary = "Not supported"
		core.Tracef("[display] %s: branch=status_unsupported", snap.ProviderID)
		return info
	}

	core.Tracef("[display] %s: checking metrics (%d total), has usage_five_hour=%v, has today_api_cost=%v, has spend_limit=%v",
		snap.ProviderID, len(snap.Metrics),
		snap.Metrics["usage_five_hour"].Used != nil,
		snap.Metrics["today_api_cost"].Used != nil,
		snap.Metrics["spend_limit"].Limit != nil)

	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil {
		remaining := *m.Limit - *m.Used
		if m.Remaining != nil {
			remaining = *m.Remaining
		}
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.reason = "spend_limit"
		info.summary = fmt.Sprintf("$%.0f / $%.0f spent", *m.Used, *m.Limit)
		info.detail = fmt.Sprintf("$%.0f remaining", remaining)
		// Add self vs team breakdown when individual spend is available
		if indiv, ok2 := snap.Metrics["individual_spend"]; ok2 && indiv.Used != nil {
			otherSpend := *m.Used - *indiv.Used
			if otherSpend < 0 {
				otherSpend = 0
			}
			info.detail = fmt.Sprintf("you $%.0f · team $%.0f · $%.0f remaining", *indiv.Used, otherSpend, remaining)
		}
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = 100 - pct
		}
		core.Tracef("[display] %s: branch=spend_limit used=%.2f limit=%.2f gauge=%.1f", snap.ProviderID, *m.Used, *m.Limit, info.gaugePercent)
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
		info.reason = "usage_five_hour"

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
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f %s", *dc.Used, tag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *dc.Used))
			}
		}
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		core.Tracef("[display] %s: branch=usage_five_hour used=%.1f gauge=%.1f → tag=Usage", snap.ProviderID, *fh.Used, info.gaugePercent)
		return info
	}

	// Billing block fallback: JSONL data confirms a 5h billing block exists
	// but Usage API percentage is unavailable. Classify as "Usage" (not "Credits").
	if _, hasBillingBlock := snap.Resets["billing_block"]; hasBillingBlock {
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
		info.reason = "billing_block_fallback"

		var parts []string
		if dc, ok2 := snap.Metrics["today_api_cost"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				parts = append(parts, fmt.Sprintf("~$%.2f %s", *dc.Used, tag))
			} else {
				parts = append(parts, fmt.Sprintf("~$%.2f", *dc.Used))
			}
		}
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			parts = append(parts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if bc, ok2 := snap.Metrics["5h_block_cost"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f 5h block", *bc.Used))
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f/%s", *wc.Used, tag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *wc.Used))
			}
		}
		if msgs, ok2 := snap.Metrics["messages_today"]; ok2 && msgs.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f msgs", *msgs.Used))
		}
		if sess, ok2 := snap.Metrics["sessions_today"]; ok2 && sess.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("%.0f sessions", *sess.Used))
		}
		info.detail = strings.Join(detailParts, " · ")
		core.Tracef("[display] %s: branch=billing_block_fallback → tag=Usage", snap.ProviderID)
		return info
	}

	if m, ok := snap.Metrics["today_api_cost"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Credits"
		info.reason = "today_api_cost"
		core.Tracef("[display] %s: branch=today_api_cost used=%.2f → tag=Credits", snap.ProviderID, *m.Used)
		tag := metricWindowTag(m)
		var costLabel string
		if tag != "" {
			costLabel = fmt.Sprintf("~$%.2f %s", *m.Used, tag)
		} else {
			costLabel = fmt.Sprintf("~$%.2f", *m.Used)
		}
		parts := []string{costLabel}
		if br, ok2 := snap.Metrics["burn_rate"]; ok2 && br.Used != nil {
			parts = append(parts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.summary = strings.Join(parts, " · ")

		var detailParts []string
		if bc, ok2 := snap.Metrics["5h_block_cost"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("~$%.2f 5h block", *bc.Used))
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			wcTag := metricWindowTag(wc)
			if wcTag != "" {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f/%s", *wc.Used, wcTag))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("~$%.2f", *wc.Used))
			}
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
		info.tagEmoji = "⚡"
		info.tagLabel = "Usage"
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
		if dc, ok2 := snap.Metrics["today_cost"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *dc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *dc.Used))
			}
		} else if dc, ok2 := snap.Metrics["usage_daily"]; ok2 && dc.Used != nil {
			tag := metricWindowTag(dc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *dc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *dc.Used))
			}
		}
		if wc, ok2 := snap.Metrics["7d_api_cost"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *wc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *wc.Used))
			}
		} else if wc, ok2 := snap.Metrics["usage_weekly"]; ok2 && wc.Used != nil {
			tag := metricWindowTag(wc)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *wc.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *wc.Used))
			}
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
			tag := metricWindowTag(daily)
			if tag != "" {
				detailParts = append(detailParts, fmt.Sprintf("%s $%.2f", tag, *daily.Used))
			} else {
				detailParts = append(detailParts, fmt.Sprintf("$%.2f", *daily.Used))
			}
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

// windowActivityLine returns a subtle summary of time-windowed telemetry activity.
// Returns "" when there is no telemetry data for the current window.
func windowActivityLine(snap core.UsageSnapshot, tw core.TimeWindow) string {
	var parts []string
	if m, ok := snap.Metrics["window_requests"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, fmt.Sprintf("%.0f reqs", *m.Used))
	}
	if m, ok := snap.Metrics["window_cost"]; ok && m.Used != nil && *m.Used > 0.001 {
		parts = append(parts, fmt.Sprintf("$%.2f", *m.Used))
	}
	if m, ok := snap.Metrics["window_tokens"]; ok && m.Used != nil && *m.Used > 0 {
		parts = append(parts, shortCompact(*m.Used)+" tok")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + " in " + tw.Label()
}

// metricWindowTag returns a short display label from a metric's Window field.
// For example "1d" → "1d", "7d" → "7d", "30d" → "30d", "all" → "all", "" → "".
func metricWindowTag(met core.Metric) string {
	w := strings.TrimSpace(met.Window)
	if w == "" {
		return ""
	}
	return w
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
		if len(rlines) > 1 {
			if bar := renderVerticalScrollBarLine(w-2, offset, h, totalLines); bar != "" {
				rlines[len(rlines)-1] = bar
			} else if end < totalLines {
				arrow := lipgloss.NewStyle().Foreground(colorAccent).Render("  ▼ more below")
				rlines[len(rlines)-1] = arrow
			}
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
	m.dashboardView = normalizeDashboardViewMode(dashboardCfg.View)

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
	m.setWidgetSections(dashboardCfg.WidgetSections)
	m.hideSectionsWithNoData = dashboardCfg.HideSectionsWithNoData
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

func (m *Model) setWidgetSections(entries []config.DashboardWidgetSection) {
	m.widgetSections = normalizeWidgetSectionEntries(entries)
	m.applyWidgetSectionOverrides()
}

func normalizeWidgetSectionEntries(entries []config.DashboardWidgetSection) []config.DashboardWidgetSection {
	if len(entries) == 0 {
		return nil
	}

	out := make([]config.DashboardWidgetSection, 0, len(entries))
	seen := make(map[core.DashboardStandardSection]bool, len(entries))
	for _, entry := range entries {
		sectionID := core.DashboardStandardSection(strings.ToLower(strings.TrimSpace(string(entry.ID))))
		sectionID = core.NormalizeDashboardStandardSection(sectionID)
		if sectionID == core.DashboardSectionHeader || !core.IsKnownDashboardStandardSection(sectionID) || seen[sectionID] {
			continue
		}
		out = append(out, config.DashboardWidgetSection{
			ID:      sectionID,
			Enabled: entry.Enabled,
		})
		seen[sectionID] = true
	}

	if len(out) == 0 {
		return nil
	}
	return out
}

func (m *Model) applyWidgetSectionOverrides() {
	entries := m.resolvedWidgetSectionEntries()
	if len(entries) == 0 {
		setDashboardWidgetSectionOverrides(nil)
		return
	}
	visible := make([]core.DashboardStandardSection, 0, len(entries))
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		visible = append(visible, entry.ID)
	}
	setDashboardWidgetSectionOverrides(visible)
}

func (m Model) defaultWidgetSectionEntries() []config.DashboardWidgetSection {
	ordered := make([]core.DashboardStandardSection, 0, len(core.DashboardStandardSections()))
	for _, section := range core.DashboardStandardSections() {
		if section == core.DashboardSectionHeader {
			continue
		}
		ordered = append(ordered, section)
	}

	entries := make([]config.DashboardWidgetSection, 0, len(ordered))
	for _, section := range ordered {
		entries = append(entries, config.DashboardWidgetSection{
			ID:      section,
			Enabled: true,
		})
	}
	return entries
}

func (m Model) widgetSectionEntries() []config.DashboardWidgetSection {
	return m.resolvedWidgetSectionEntries()
}

func (m Model) resolvedWidgetSectionEntries() []config.DashboardWidgetSection {
	if len(m.widgetSections) == 0 {
		return m.defaultWidgetSectionEntries()
	}

	out := make([]config.DashboardWidgetSection, len(m.widgetSections))
	copy(out, m.widgetSections)

	seen := make(map[core.DashboardStandardSection]bool, len(out))
	for _, entry := range out {
		seen[entry.ID] = true
	}
	for _, entry := range m.defaultWidgetSectionEntries() {
		if seen[entry.ID] {
			continue
		}
		out = append(out, entry)
	}

	return out
}

func (m *Model) setWidgetSectionEntries(entries []config.DashboardWidgetSection) {
	normalized := normalizeWidgetSectionEntries(entries)
	m.widgetSections = normalized
	m.applyWidgetSectionOverrides()
}

func (m Model) dashboardWidgetSectionConfigEntries() []config.DashboardWidgetSection {
	if len(m.widgetSections) == 0 {
		return nil
	}
	out := make([]config.DashboardWidgetSection, len(m.widgetSections))
	copy(out, m.widgetSections)
	return out
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
	m.settings.integrationStatus = manager.ListStatuses()
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
	if m.filter.text == "" {
		return m.sortedIDs
	}
	lower := strings.ToLower(m.filter.text)
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
