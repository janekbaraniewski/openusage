package tui

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	tileBodyCache         map[string][]string
	analyticsCache        analyticsRenderCacheEntry
	detailCache           detailRenderCacheEntry

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
		tileBodyCache:         make(map[string][]string),
		analyticsCache:        analyticsRenderCacheEntry{},
		detailCache:           detailRenderCacheEntry{},
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
	keys := core.SortedStringKeys(m.snapshots)

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
	m.invalidateTileBodyCache()
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
	m.invalidateTileBodyCache()
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

	return core.SortedStringKeys(seen)
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

	return core.SortedStringKeys(seen)
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

	return core.SortedStringKeys(seen)
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

	extra := lo.Filter(core.SortedStringKeys(m.snapshots), func(id string, _ int) bool {
		return !seen[id] && m.isProviderEnabled(id)
	})

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
