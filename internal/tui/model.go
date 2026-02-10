package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/agentusage/internal/core"
)

// tickMsg drives animations (gradient brand, spinner, pulse).
type tickMsg time.Time

// tickCmd returns the next animation tick command.
func tickCmd() tea.Cmd {
	return tea.Tick(150*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// ─── Screen Tabs (tmux-style) ───────────────────────────────────────────────

type screenTab int

const (
	screenDashboard screenTab = iota // tiles grid overview
	screenList                       // master-detail list
	screenAnalytics                  // spend analysis dashboard
	screenCount                      // sentinel for cycling
)

var screenLabels = []string{"Dashboard", "List", "Analytics"}

// ─── View Modes ─────────────────────────────────────────────────────────────

type viewMode int

const (
	modeList   viewMode = iota // navigating the provider list (left panel focus)
	modeDetail                 // scrolling the detail panel (right panel focus)
)

const (
	minLeftWidth = 28
	maxLeftWidth = 38
)

// ─── Messages ───────────────────────────────────────────────────────────────

// SnapshotsMsg is sent by the engine when fresh snapshots arrive.
type SnapshotsMsg map[string]core.QuotaSnapshot

// ─── Model ──────────────────────────────────────────────────────────────────

// Model is the Bubble Tea model for the AgentUsage TUI.
type Model struct {
	snapshots map[string]core.QuotaSnapshot
	sortedIDs []string
	cursor    int
	mode      viewMode
	filter    string
	filtering bool
	showHelp  bool
	width     int
	height    int

	detailOffset int // vertical scroll offset for the detail panel
	detailTab    int // active tab index in the detail panel (0=All)

	warnThreshold float64
	critThreshold float64

	// Screen tabs (tmux-style)
	screen screenTab

	// Analytics state
	analyticsScroll    int
	analyticsFilter    string
	analyticsFiltering bool
	analyticsSortBy    int // 0=cost↓, 1=name↑, 2=tokens↓

	// Animation state
	animFrame  int  // monotonically increasing frame counter
	refreshing bool // true when a manual refresh is in progress
}

// NewModel creates a new TUI model.
func NewModel(warnThresh, critThresh float64) Model {
	return Model{
		snapshots:     make(map[string]core.QuotaSnapshot),
		warnThreshold: warnThresh,
		critThreshold: critThresh,
	}
}

// ─── Bubble Tea Interface ───────────────────────────────────────────────────

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
		m.rebuildSortedIDs()
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

// ─── Keyboard Handling ──────────────────────────────────────────────────────

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Help toggle (works from anywhere)
	if msg.String() == "?" && !m.filtering && !m.analyticsFiltering {
		m.showHelp = !m.showHelp
		return m, nil
	}
	// Dismiss help on any key when help is shown
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}

	// Screen tab switching: Tab / Shift+Tab cycle through screens
	// Theme cycling: t key
	if !m.filtering && !m.analyticsFiltering {
		switch msg.String() {
		case "tab":
			m.screen = (m.screen + 1) % screenCount
			m.mode = modeList
			m.detailOffset = 0
			return m, nil
		case "shift+tab":
			m.screen = (m.screen - 1 + screenCount) % screenCount
			m.mode = modeList
			m.detailOffset = 0
			return m, nil
		case "t":
			CycleTheme()
			return m, nil
		}
	}

	// Dispatch to current screen handler
	switch m.screen {
	case screenAnalytics:
		return m.handleAnalyticsKey(msg)
	case screenDashboard:
		return m.handleDashboardTilesKey(msg)
	default:
		return m.handleDashboardListKey(msg)
	}
}

// handleDashboardTilesKey handles keyboard input for the tiles grid screen.
func (m Model) handleDashboardTilesKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	if m.mode == modeDetail {
		return m.handleDetailKey(msg)
	}
	return m.handleTilesKey(msg)
}

// handleDashboardListKey handles keyboard input for the master-detail list screen.
func (m Model) handleDashboardListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		return m.handleFilterKey(msg)
	}
	switch m.mode {
	case modeList:
		return m.handleListKey(msg)
	case modeDetail:
		return m.handleDetailKey(msg)
	}
	return m, nil
}

// handleAnalyticsKey handles keyboard input for the analytics screen.
func (m Model) handleAnalyticsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.analyticsFiltering {
		return m.handleAnalyticsFilterKey(msg)
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		if m.analyticsScroll > 0 {
			m.analyticsScroll--
		}
	case "down", "j":
		m.analyticsScroll++
	case "s":
		m.analyticsSortBy = (m.analyticsSortBy + 1) % analyticsSortCount
		m.analyticsScroll = 0
	case "G":
		m.analyticsScroll = 9999
	case "g":
		m.analyticsScroll = 0
	case "/":
		m.analyticsFiltering = true
		m.analyticsFilter = ""
	case "esc":
		if m.analyticsFilter != "" {
			m.analyticsFilter = ""
			m.analyticsScroll = 0
		}
	case "r":
		m.refreshing = true
	}
	return m, nil
}

// handleAnalyticsFilterKey handles keyboard input while typing an analytics filter.
func (m Model) handleAnalyticsFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter", "esc":
		m.analyticsFiltering = false
		m.analyticsScroll = 0
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
		}
	case "down", "j":
		if m.cursor < len(ids)-1 {
			m.cursor++
			m.detailOffset = 0
			m.detailTab = 0
		}
	case "enter", "right", "l":
		m.mode = modeDetail
		m.detailOffset = 0
	case "/":
		m.filtering = true
		m.filter = ""
	case "r":
		m.refreshing = true
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
		// Previous tab
		if m.detailTab > 0 {
			m.detailTab--
			m.detailOffset = 0
		}
	case "]":
		// Next tab — capped by available tabs during render
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
	case "enter", "esc":
		m.filtering = false
		m.cursor = 0
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
		}
	case "down", "j":
		if m.cursor+cols < len(ids) {
			m.cursor += cols
		}
	case "left", "h":
		if m.cursor > 0 {
			m.cursor--
		}
	case "right", "l":
		if m.cursor < len(ids)-1 {
			m.cursor++
		}
	case "enter":
		m.mode = modeDetail
		m.detailOffset = 0
	case "/":
		m.filtering = true
		m.filter = ""
	case "r":
		m.refreshing = true
	}
	return m, nil
}

// ─── View ───────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width < 30 || m.height < 8 {
		return lipgloss.NewStyle().
			Foreground(colorDim).
			Render("\n  Terminal too small. Resize to at least 30×8.")
	}
	if m.showHelp {
		return m.renderHelpOverlay(m.width, m.height)
	}
	return m.renderDashboard()
}

func (m Model) renderDashboard() string {
	w, h := m.width, m.height

	// Header
	header := m.renderHeader(w)
	headerH := strings.Count(header, "\n") + 1

	// Footer
	footer := m.renderFooter(w)
	footerH := strings.Count(footer, "\n") + 1

	// Content area
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

// renderDashboardContent renders the provider list / tiles / detail content area.
func (m Model) renderDashboardContent(w, contentH int) string {
	// If in detail mode (entered via Enter from tiles or list), show detail
	if m.mode == modeDetail {
		if w >= 65 && m.screen == screenList {
			// Wide list screen: side-by-side master-detail
			leftW := clamp(w/3, minLeftWidth, maxLeftWidth)
			rightW := w - leftW - 1
			left := m.renderList(leftW, contentH)
			sep := renderVerticalSep(contentH)
			right := m.renderDetailPanel(rightW, contentH)
			return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		}
		// Tiles detail or narrow: full-screen detail
		return m.renderDetailPanel(w, contentH)
	}

	switch m.screen {
	case screenDashboard:
		// Tiles grid view
		return m.renderTiles(w, contentH)
	default:
		// List view
		if w >= 65 {
			leftW := clamp(w/3, minLeftWidth, maxLeftWidth)
			rightW := w - leftW - 1
			left := m.renderList(leftW, contentH)
			sep := renderVerticalSep(contentH)
			right := m.renderDetailPanel(rightW, contentH)
			return lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
		}
		return m.renderList(w, contentH)
	}
}

// ─── Header & Footer ────────────────────────────────────────────────────────

func (m Model) renderHeader(w int) string {
	// ── Animated brand with color wave ──
	bolt := PulseChar(
		lipgloss.NewStyle().Foreground(colorAccent).Bold(true).Render("⚡"),
		lipgloss.NewStyle().Foreground(colorDim).Bold(true).Render("⚡"),
		m.animFrame,
	)
	brandText := RenderGradientText("AgentUsage", m.animFrame)

	// ── Screen tabs (tmux-style) ──
	tabs := m.renderScreenTabs()

	// ── Spinner when refreshing ──
	spinnerStr := ""
	if m.refreshing {
		frame := m.animFrame % len(SpinnerFrames)
		spinnerStr = " " + lipgloss.NewStyle().Foreground(colorAccent).Render(SpinnerFrames[frame])
	}

	// ── Right side info ──
	var info string

	switch m.screen {
	case screenAnalytics:
		if m.analyticsFiltering {
			cursor := PulseChar("█", "▌", m.animFrame)
			info = dimStyle.Render("search: ") + lipgloss.NewStyle().Foreground(colorSapphire).Render(m.analyticsFilter+cursor)
		} else if m.analyticsFilter != "" {
			info = dimStyle.Render("filtered: ") + lipgloss.NewStyle().Foreground(colorSapphire).Render(m.analyticsFilter)
		} else {
			info = dimStyle.Render("spend analysis")
		}
	default:
		ids := m.filteredIDs()
		info = fmt.Sprintf("⊞ %d providers", len(ids))

		if m.filtering {
			cursor := PulseChar("█", "▌", m.animFrame)
			info = dimStyle.Render("search: ") + lipgloss.NewStyle().Foreground(colorSapphire).Render(m.filter+cursor)
		} else if m.filter != "" {
			info = dimStyle.Render("filtered: ") + lipgloss.NewStyle().Foreground(colorSapphire).Render(m.filter)
		}
	}

	// ── Status indicators with pulse ──
	ids := m.filteredIDs()
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

	infoRendered := lipgloss.NewStyle().Foreground(colorSubtext).Render(info)

	left := bolt + " " + brandText + " " + tabs + statusInfo + spinnerStr
	gap := w - lipgloss.Width(left) - lipgloss.Width(infoRendered)
	if gap < 1 {
		gap = 1
	}

	line := left + strings.Repeat(" ", gap) + infoRendered

	// ── Separator with accent fade ──
	sep := m.renderGradientSeparator(w)

	return line + "\n" + sep
}

// renderGradientSeparator renders a separator line with a subtle accent glow.
func (m Model) renderGradientSeparator(w int) string {
	if w <= 0 {
		return ""
	}
	// Build a separator that has a subtle accent glow in the center
	dimBar := lipgloss.NewStyle().Foreground(colorSurface1)
	accentBar := lipgloss.NewStyle().Foreground(colorAccent)

	if w < 20 {
		return dimBar.Render(strings.Repeat("━", w))
	}

	// Animated glow position
	glowW := 8
	pos := (m.animFrame * 2) % (w + glowW)

	var b strings.Builder
	for i := 0; i < w; i++ {
		dist := i - (pos - glowW/2)
		if dist < 0 {
			dist = -dist
		}
		if dist <= glowW/2 {
			b.WriteString(accentBar.Render("━"))
		} else {
			b.WriteString(dimBar.Render("━"))
		}
	}
	return b.String()
}

// renderScreenTabs renders tmux-style screen tab indicators.
func (m Model) renderScreenTabs() string {
	var parts []string
	for i, label := range screenLabels {
		tabStr := fmt.Sprintf("%d:%s", i+1, label)
		if screenTab(i) == m.screen {
			parts = append(parts, screenTabActiveStyle.Render(tabStr))
		} else {
			parts = append(parts, screenTabInactiveStyle.Render(tabStr))
		}
	}
	return strings.Join(parts, "")
}

func (m Model) renderFooter(w int) string {
	sep := lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("━", w))

	var keys []string

	// Screen tab hint
	tabHint := helpKeyStyle.Render("⇥") + helpStyle.Render(" tab")
	themeHint := helpKeyStyle.Render("t") + helpStyle.Render(" theme")

	switch {
	case m.screen == screenAnalytics:
		keys = []string{
			helpKeyStyle.Render("↑↓") + helpStyle.Render(" scroll"),
			helpKeyStyle.Render("g/G") + helpStyle.Render(" top/btm"),
			helpKeyStyle.Render("s") + helpStyle.Render(" sort"),
			helpKeyStyle.Render("/") + helpStyle.Render(" filter"),
			tabHint,
			themeHint,
			helpKeyStyle.Render("?") + helpStyle.Render(" help"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	case m.mode == modeDetail:
		keys = []string{
			helpKeyStyle.Render("[/]") + helpStyle.Render(" tab"),
			helpKeyStyle.Render("↑↓") + helpStyle.Render(" scroll"),
			helpKeyStyle.Render("g/G") + helpStyle.Render(" top/btm"),
			helpKeyStyle.Render("esc") + helpStyle.Render(" back"),
			themeHint,
			helpKeyStyle.Render("?") + helpStyle.Render(" help"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	case m.screen == screenDashboard:
		keys = []string{
			helpKeyStyle.Render("←↑↓→") + helpStyle.Render(" nav"),
			helpKeyStyle.Render("⏎") + helpStyle.Render(" detail"),
			helpKeyStyle.Render("/") + helpStyle.Render(" filter"),
			tabHint,
			themeHint,
			helpKeyStyle.Render("?") + helpStyle.Render(" help"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	default:
		keys = []string{
			helpKeyStyle.Render("↑↓") + helpStyle.Render(" nav"),
			helpKeyStyle.Render("⏎") + helpStyle.Render(" detail"),
			helpKeyStyle.Render("/") + helpStyle.Render(" filter"),
			tabHint,
			themeHint,
			helpKeyStyle.Render("?") + helpStyle.Render(" help"),
			helpKeyStyle.Render("q") + helpStyle.Render(" quit"),
		}
	}

	help := " " + strings.Join(keys, "   ")

	// Theme name indicator (right-aligned)
	themeName := lipgloss.NewStyle().Foreground(colorAccent).Render(ThemeName())
	helpW := lipgloss.Width(help)
	themeW := lipgloss.Width(themeName)
	gap := w - helpW - themeW - 1
	if gap < 1 {
		// Not enough room for theme name
		return sep + "\n" + help
	}

	return sep + "\n" + help + strings.Repeat(" ", gap) + themeName
}

// ─── Left Panel: Provider List ──────────────────────────────────────────────

func (m Model) renderList(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 {
		empty := []string{
			"",
			dimStyle.Render("  No providers detected."),
			"",
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  Set API-key env vars"),
			lipgloss.NewStyle().Foreground(colorSubtext).Render("  or install AI tools."),
		}
		return padToSize(strings.Join(empty, "\n"), w, h)
	}

	// Scrollable list
	itemHeight := 3 // each item is 3 lines (name + summary + separator)
	visibleItems := h / itemHeight
	if visibleItems < 1 {
		visibleItems = 1
	}

	// Calculate scroll window
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

	// Scroll indicators
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

// renderListItem renders a single provider row in the list panel.
//
// Layout (3 lines per item):
//
//	Line 1:  ● provider-name       🔥 Cost  OK
//	Line 2:    $4.23 today · $0.82/h    ━━━━━━ 62%
//	Line 3:  ──────────────────────────────────────
//
// The category tag (emoji + label) is shown between the name and badge,
// giving immediate context about what kind of data this provider exposes.
func (m Model) renderListItem(snap core.QuotaSnapshot, selected bool, w int) string {
	di := computeDisplayInfo(snap)

	icon := StatusIcon(snap.Status)
	iconColor := StatusColor(snap.Status)
	iconStr := lipgloss.NewStyle().Foreground(iconColor).Render(icon)

	nameStyle := lipgloss.NewStyle().Foreground(colorText)
	if selected {
		nameStyle = nameStyle.Bold(true).Foreground(colorLavender)
	}

	// Build right side: tag + badge
	badge := StatusBadge(snap.Status)
	var tagRendered string
	if di.tagEmoji != "" && di.tagLabel != "" {
		tc := tagColor(di.tagLabel)
		tagRendered = lipgloss.NewStyle().Foreground(tc).Render(di.tagEmoji+" "+di.tagLabel) + " "
	}
	rightPart := tagRendered + badge
	rightW := lipgloss.Width(rightPart)

	// Truncate name to fit
	name := snap.AccountID
	maxName := w - rightW - 6 // icon + spaces + gap
	if maxName < 5 {
		maxName = 5
	}
	if len(name) > maxName {
		name = name[:maxName-1] + "…"
	}

	// Line 1: icon + name ··· tag + badge
	namePart := fmt.Sprintf(" %s %s", iconStr, nameStyle.Render(name))
	nameW := lipgloss.Width(namePart)
	gapLen := w - nameW - rightW - 1
	if gapLen < 1 {
		gapLen = 1
	}
	line1 := namePart + strings.Repeat(" ", gapLen) + rightPart

	// Line 2: summary + mini gauge
	summary := di.summary
	summaryStyle := lipgloss.NewStyle().Foreground(colorSubtext)

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

	// Line 3: thin separator
	line3 := "  " + lipgloss.NewStyle().Foreground(colorSurface1).Render(strings.Repeat("─", w-4))

	result := line1 + "\n" + line2 + "\n" + line3

	// Selected indicator: colored bar on left edge
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

// ─── Provider Display Info ──────────────────────────────────────────────────

// providerDisplayInfo holds computed display data for a provider snapshot,
// ensuring consistent rendering across tiles and list views.
// Inspired by btop's semantic gauges, k9s's color-coded status columns,
// and clig.dev guidelines (pair color with text labels, structured output).
type providerDisplayInfo struct {
	tagEmoji     string  // "💰", "⚡", "📊", "🔥", "💬", "💳", "⏱"
	tagLabel     string  // "Spend", "Rate", "Quota", "Cost", "Activity", "Credits"
	summary      string  // Primary summary (e.g. "$4.23 today · $0.82/h")
	detail       string  // Secondary detail (e.g. "Primary 3% · Secondary 15%")
	gaugePercent float64 // 0-100 remaining %. -1 if not applicable.
}

// computeDisplayInfo extracts the most meaningful display data from a snapshot
// using a priority chain. It NEVER falls through to "N metrics" — every provider
// gets a useful, contextualized summary with an appropriate category tag.
func computeDisplayInfo(snap core.QuotaSnapshot) providerDisplayInfo {
	info := providerDisplayInfo{gaugePercent: -1}

	// ── Error / auth / unsupported states ──
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

	// ── Priority 1: Spend limit (cursor team/pooled budget) ──
	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && m.Used != nil {
		remaining := *m.Limit - *m.Used
		if m.Remaining != nil {
			remaining = *m.Remaining
		}
		info.tagEmoji = "💰"
		info.tagLabel = "Spend"
		info.summary = fmt.Sprintf("$%.0f / $%.0f spent", *m.Used, *m.Limit)
		info.detail = fmt.Sprintf("$%.0f remaining", remaining)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = pct
		}
		return info
	}

	// ── Priority 2: Plan spend with limit ──
	if m, ok := snap.Metrics["plan_spend"]; ok && m.Used != nil && m.Limit != nil {
		info.tagEmoji = "📊"
		info.tagLabel = "Plan"
		info.summary = fmt.Sprintf("$%.0f / $%.0f plan", *m.Used, *m.Limit)
		if pct := m.Percent(); pct >= 0 {
			info.gaugePercent = pct
		}
		if pu, ok2 := snap.Metrics["plan_percent_used"]; ok2 && pu.Used != nil {
			info.detail = fmt.Sprintf("%.0f%% plan used", *pu.Used)
		}
		return info
	}

	// ── Priority 2b: Plan total spend (OpenRouter etc.) ──
	if m, ok := snap.Metrics["plan_total_spend_usd"]; ok && m.Used != nil {
		info.tagEmoji = "📊"
		info.tagLabel = "Plan"
		if lm, ok2 := snap.Metrics["plan_limit_usd"]; ok2 && lm.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.0f plan", *m.Used, *lm.Limit)
		} else {
			info.summary = fmt.Sprintf("$%.2f spent", *m.Used)
		}
		return info
	}

	// ── Priority 3: Credits / balance ──
	if m, ok := snap.Metrics["credits"]; ok {
		info.tagEmoji = "💳"
		info.tagLabel = "Credits"
		if m.Remaining != nil && m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f credits", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = pct
			}
		} else if m.Used != nil {
			info.summary = fmt.Sprintf("$%.4f used", *m.Used)
		} else {
			info.summary = "Credits available"
		}
		return info
	}
	if m, ok := snap.Metrics["credit_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💳"
		info.tagLabel = "Credits"
		if m.Limit != nil {
			info.summary = fmt.Sprintf("$%.2f / $%.2f", *m.Remaining, *m.Limit)
			if pct := m.Percent(); pct >= 0 {
				info.gaugePercent = pct
			}
		} else {
			info.summary = fmt.Sprintf("$%.2f balance", *m.Remaining)
		}
		return info
	}
	if m, ok := snap.Metrics["total_balance"]; ok && m.Remaining != nil {
		info.tagEmoji = "💳"
		info.tagLabel = "Balance"
		info.summary = fmt.Sprintf("%.2f %s available", *m.Remaining, m.Unit)
		return info
	}

	// ── Priority 4: Rate limits (codex, openai, anthropic, groq, etc.) ──
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
		// Build detail parts
		if m.Unit == "%" && m.Remaining != nil {
			label := prettifyKey(strings.TrimPrefix(key, "rate_limit_"))
			rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", label, *m.Remaining))
		} else if pct >= 0 {
			label := strings.ToUpper(key)
			rateParts = append(rateParts, fmt.Sprintf("%s %.0f%%", label, pct))
		}
	}
	if hasRateLimits {
		info.tagEmoji = "⚡"
		info.tagLabel = "Rate"
		info.gaugePercent = worstRatePct
		info.summary = fmt.Sprintf("%.0f%% remaining", worstRatePct)
		if len(rateParts) > 0 {
			sort.Strings(rateParts)
			info.detail = strings.Join(rateParts, " · ")
		}
		return info
	}

	// ── Priority 5: Daily cost (claude-code etc.) ──
	if m, ok := snap.Metrics["daily_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "🔥"
		info.tagLabel = "Cost"
		parts := []string{fmt.Sprintf("$%.2f today", *m.Used)}
		if br, ok2 := snap.Metrics["burn_rate_usd_per_hour"]; ok2 && br.Used != nil {
			parts = append(parts, fmt.Sprintf("$%.2f/h", *br.Used))
		}
		info.summary = strings.Join(parts, " · ")
		// Build detail from block + activity
		var detailParts []string
		if bc, ok2 := snap.Metrics["block_cost_usd"]; ok2 && bc.Used != nil {
			detailParts = append(detailParts, fmt.Sprintf("$%.2f block", *bc.Used))
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

	// ── Priority 6: Block cost ──
	if m, ok := snap.Metrics["block_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "⏱"
		info.tagLabel = "Block"
		info.summary = fmt.Sprintf("$%.2f / 5h block", *m.Used)
		if br, ok2 := snap.Metrics["burn_rate_usd_per_hour"]; ok2 && br.Used != nil {
			info.detail = fmt.Sprintf("$%.2f/h burn rate", *br.Used)
		}
		return info
	}

	// ── Priority 7: Quota remaining % (gemini, generic) ──
	hasQuota := false
	worstQuotaPct := float64(100)
	var quotaKey string
	for key, m := range snap.Metrics {
		pct := m.Percent()
		if pct >= 0 {
			hasQuota = true
			if pct < worstQuotaPct {
				worstQuotaPct = pct
				quotaKey = key
			}
		}
	}
	if hasQuota {
		info.tagEmoji = "📊"
		info.tagLabel = "Quota"
		info.gaugePercent = worstQuotaPct
		info.summary = fmt.Sprintf("%.0f%% remaining", worstQuotaPct)
		if quotaKey != "" {
			qm := snap.Metrics[quotaKey]
			parts := []string{prettifyKey(quotaKey)}
			if qm.Window != "" && qm.Window != "all_time" && qm.Window != "current_period" {
				parts = append(parts, qm.Window)
			}
			info.detail = strings.Join(parts, " · ")
		}
		return info
	}

	// ── Priority 8: Total cost ──
	if m, ok := snap.Metrics["total_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Cost"
		info.summary = fmt.Sprintf("$%.2f total", *m.Used)
		return info
	}
	if m, ok := snap.Metrics["jsonl_total_cost_usd"]; ok && m.Used != nil {
		info.tagEmoji = "💰"
		info.tagLabel = "Cost"
		info.summary = fmt.Sprintf("$%.2f total (est.)", *m.Used)
		return info
	}

	// ── Priority 9: Messages / activity today ──
	if m, ok := snap.Metrics["messages_today"]; ok && m.Used != nil {
		info.tagEmoji = "💬"
		info.tagLabel = "Activity"
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

	// ── Priority 10: Any metric with a Used value ──
	for key, m := range snap.Metrics {
		if m.Used != nil {
			info.tagEmoji = "📋"
			info.tagLabel = "Metrics"
			info.summary = fmt.Sprintf("%s: %s %s", prettifyKey(key), formatNumber(*m.Used), m.Unit)
			return info
		}
	}

	// ── Priority 11: Message ──
	if snap.Message != "" {
		info.tagEmoji = "ℹ"
		info.tagLabel = "Info"
		msg := snap.Message
		if len(msg) > 50 {
			msg = msg[:47] + "..."
		}
		info.summary = msg
		return info
	}

	// ── Fallback — NEVER "N metrics" ──
	info.tagEmoji = "·"
	info.tagLabel = ""
	info.summary = string(snap.Status)
	return info
}

// providerSummary returns a one-line summary for the provider list.
// Delegates to computeDisplayInfo for consistent output.
func providerSummary(snap core.QuotaSnapshot) string {
	return computeDisplayInfo(snap).summary
}

// bestMetricPercent finds the most meaningful remaining % from rate limit metrics,
// skipping plan_percent_used when a spend limit exists (since plan can be 100%
// used while spend limit has ample budget).
func bestMetricPercent(snap core.QuotaSnapshot) float64 {
	hasSpendLimit := false
	if m, ok := snap.Metrics["spend_limit"]; ok && m.Limit != nil && *m.Limit > 0 {
		hasSpendLimit = true
	}

	worst := float64(100)
	found := false
	for key, m := range snap.Metrics {
		// Skip plan percent if we have a spend limit
		if hasSpendLimit && (key == "plan_percent_used" || key == "plan_spend") {
			continue
		}
		p := m.Percent()
		if p >= 0 {
			found = true
			if p < worst {
				worst = p
			}
		}
	}
	if !found {
		return -1
	}
	return worst
}

// ─── Right Panel: Detail ────────────────────────────────────────────────────

func (m Model) renderDetailPanel(w, h int) string {
	ids := m.filteredIDs()
	if len(ids) == 0 || m.cursor >= len(ids) {
		return padToSize("", w, h)
	}

	snap := m.snapshots[ids[m.cursor]]

	// Clamp the active tab to available tabs
	tabs := DetailTabs(snap)
	activeTab := m.detailTab
	if activeTab >= len(tabs) {
		activeTab = len(tabs) - 1
	}
	if activeTab < 0 {
		activeTab = 0
	}

	content := RenderDetailContent(snap, w-2, m.warnThreshold, m.critThreshold, activeTab)

	// Apply vertical scrolling
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

	// Pad to height
	for len(visible) < h {
		visible = append(visible, "")
	}

	result := strings.Join(visible, "\n")

	// Scroll indicators
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

// ─── Separator ──────────────────────────────────────────────────────────────

func renderVerticalSep(h int) string {
	style := lipgloss.NewStyle().Foreground(colorSurface1)
	lines := make([]string, h)
	for i := range lines {
		lines[i] = style.Render("┃")
	}
	return strings.Join(lines, "\n")
}

// ─── Helpers ────────────────────────────────────────────────────────────────

func (m *Model) rebuildSortedIDs() {
	m.sortedIDs = make([]string, 0, len(m.snapshots))
	for id := range m.snapshots {
		m.sortedIDs = append(m.sortedIDs, id)
	}
	sort.Strings(m.sortedIDs)
}

func (m Model) filteredIDs() []string {
	if m.filter == "" {
		return m.sortedIDs
	}
	lower := strings.ToLower(m.filter)
	var out []string
	for _, id := range m.sortedIDs {
		snap := m.snapshots[id]
		if strings.Contains(strings.ToLower(id), lower) ||
			strings.Contains(strings.ToLower(snap.ProviderID), lower) ||
			strings.Contains(strings.ToLower(string(snap.Status)), lower) {
			out = append(out, id)
		}
	}
	return out
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
