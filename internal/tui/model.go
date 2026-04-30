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

// Adaptive tick intervals to reduce CPU/power when idle.
const (
	tickFast   = 150 * time.Millisecond // loading: spinner/shimmer animations
	tickNormal = 500 * time.Millisecond // recently active: smooth animations
	tickSlow   = 2 * time.Second        // data recently changed: minimal animation
	// When fully idle, ticking stops entirely (no CPU wake-ups).

	idleAfterInteraction = 5 * time.Second  // fast→normal→slow after no user input
	idleAfterData        = 15 * time.Second // slow→paused after no data change
)

func tickCmd() tea.Cmd {
	return scheduleTickCmd(tickFast)
}

func scheduleTickCmd(interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(t time.Time) tea.Msg {
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
	sectionSubTab     int // 0=tile sections, 1=detail sections
	previewOffset     int
	status            string
	integrationStatus []integrations.Status

	apiKeyEditing       bool
	apiKeyInput         string
	apiKeyEditAccountID string
	apiKeyStatus        string // "validating...", "valid ✓", "invalid ✗", etc.

	providerLinkPicker providerLinkPickerState
}

// providerLinkPickerState tracks the in-modal target picker for a telemetry
// provider. When active, key input on the TELEM tab is routed to the picker
// (up/down to choose, enter to apply, esc to cancel).
type providerLinkPickerState struct {
	active  bool
	source  string
	choices []string
	cursor  int
	status  string
}

type Services interface {
	SaveTheme(themeName string) error
	SaveDashboardProviders(providers []config.DashboardProviderConfig) error
	SaveDashboardView(view string) error
	SaveDashboardWidgetSections(sections []config.DashboardWidgetSection) error
	SaveDetailWidgetSections(sections []config.DetailWidgetSection) error
	SaveDashboardHideSectionsWithNoData(hide bool) error
	SaveTimeWindow(window string) error
	SaveProviderLink(source, target string) error
	DeleteProviderLink(source string) error
	ConnectBrowserSession(accountID, domain, cookieName, preferredBrowser string) (core.BrowserSessionInfo, error)
	DisconnectBrowserSession(accountID string) error
	LoadBrowserSessionInfo(accountID string) core.BrowserSessionInfo
	OpenProviderConsole(url string) error
	AvailableBrowsers() ([]string, error)
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

	analyticsFilter      filterState
	analyticsSortBy      int             // 0=cost↓, 1=name↑, 2=tokens↓
	analyticsTab         int             // 0=overview, 1=models, 2=spend, 3=activity
	analyticsModelCursor int             // selected model index in the Models tab
	analyticsModelExpand map[string]bool // expanded models in the Models tab
	analyticsScrollY     int             // vertical scroll offset for analytics content

	animFrame  int // monotonically increasing frame counter
	refreshing bool
	hasData    bool

	tickRunning     bool      // true while the tick chain is active
	lastInteraction time.Time // last user keypress/mouse event
	lastDataUpdate  time.Time // last SnapshotsMsg with new data

	experimentalAnalytics bool // when false, only the Dashboard screen is available

	daemon daemonState

	providerOrder    []string
	providerEnabled  map[string]bool
	accountProviders map[string]string

	settings               settingsState
	widgetSections         []config.DashboardWidgetSection
	detailWidgetSections   []config.DetailWidgetSection
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
		analyticsModelExpand:  make(map[string]bool),
		analyticsCache:        analyticsRenderCacheEntry{},
		detailCache:           detailRenderCacheEntry{},
		daemon:                daemonState{status: DaemonConnecting},
		timeWindow:            timeWindow,
		tickRunning:           true, // Init() starts the first tick chain
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
type detailWidgetSectionsPersistedMsg struct {
	err error
}
type dashboardHideSectionsWithNoDataPersistedMsg struct {
	err error
}
type timeWindowPersistedMsg struct {
	err error
}
type providerLinkPersistedMsg struct {
	source string
	target string
	err    error
}
type providerLinkDeletedMsg struct {
	source string
	err    error
}

// browserSessionConnectedMsg is emitted by connectBrowserSessionCmd. On
// success Info carries the captured (domain, cookie_name, source_browser,
// captured_at, expires_at) tuple — the cookie value is never marshalled
// into TUI message types. Err is non-nil when extraction fails (no cookie
// in any browser, keychain prompt declined, etc.).
type browserSessionConnectedMsg struct {
	AccountID string
	Info      core.BrowserSessionInfo
	Err       error
}

type browserSessionDisconnectedMsg struct {
	AccountID string
	Err       error
}

type providerConsoleOpenedMsg struct {
	URL string
	Err error
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

// nextTickInterval determines the appropriate tick interval based on activity.
// Returns 0 when the tick chain should stop (fully idle).
func (m Model) nextTickInterval() time.Duration {
	// Loading state: fast tick for spinner/shimmer animations.
	if !m.hasData || m.refreshing {
		return tickFast
	}

	now := time.Now()

	// Recent user interaction: normal animation speed.
	if !m.lastInteraction.IsZero() && now.Sub(m.lastInteraction) < idleAfterInteraction {
		return tickNormal
	}

	// Data recently changed: slow tick for status indicators.
	if !m.lastDataUpdate.IsZero() && now.Sub(m.lastDataUpdate) < idleAfterData {
		return tickSlow
	}

	// Fully idle: stop ticking. The chain restarts on the next message.
	return 0
}

// restartTickIfNeeded returns a tick command if the tick chain is not running.
// Call this from message handlers that should wake the UI from idle.
func (m *Model) restartTickIfNeeded() tea.Cmd {
	if m.tickRunning {
		return nil
	}
	m.tickRunning = true
	return scheduleTickCmd(tickNormal)
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
	m.setDetailWidgetSections(dashboardCfg.DetailSections)
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

func (m *Model) setDetailWidgetSections(entries []config.DetailWidgetSection) {
	m.detailWidgetSections = normalizeDetailWidgetSectionEntries(entries)
	m.applyDetailWidgetSectionOverrides()
	m.invalidateDetailCache()
}

func normalizeDetailWidgetSectionEntries(entries []config.DetailWidgetSection) []config.DetailWidgetSection {
	if len(entries) == 0 {
		return nil
	}

	out := make([]config.DetailWidgetSection, 0, len(entries))
	seen := make(map[core.DetailStandardSection]bool, len(entries))
	for _, entry := range entries {
		sectionID := core.DetailStandardSection(strings.ToLower(strings.TrimSpace(string(entry.ID))))
		if !core.IsKnownDetailStandardSection(sectionID) || seen[sectionID] {
			continue
		}
		out = append(out, config.DetailWidgetSection{
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

func (m *Model) applyDetailWidgetSectionOverrides() {
	entries := m.resolvedDetailWidgetSectionEntries()
	if len(entries) == 0 {
		setDetailSectionOverrides(nil)
		return
	}
	visible := make([]core.DetailStandardSection, 0, len(entries))
	for _, entry := range entries {
		if !entry.Enabled {
			continue
		}
		visible = append(visible, entry.ID)
	}
	setDetailSectionOverrides(visible)
}

func (m Model) defaultDetailWidgetSectionEntries() []config.DetailWidgetSection {
	ordered := core.DefaultDetailSectionOrder()
	entries := make([]config.DetailWidgetSection, 0, len(ordered))
	for _, section := range ordered {
		entries = append(entries, config.DetailWidgetSection{
			ID:      section,
			Enabled: true,
		})
	}
	return entries
}

func (m Model) detailWidgetSectionEntries() []config.DetailWidgetSection {
	return m.resolvedDetailWidgetSectionEntries()
}

func (m Model) resolvedDetailWidgetSectionEntries() []config.DetailWidgetSection {
	if len(m.detailWidgetSections) == 0 {
		return m.defaultDetailWidgetSectionEntries()
	}

	out := make([]config.DetailWidgetSection, len(m.detailWidgetSections))
	copy(out, m.detailWidgetSections)

	seen := make(map[core.DetailStandardSection]bool, len(out))
	for _, entry := range out {
		seen[entry.ID] = true
	}
	for _, entry := range m.defaultDetailWidgetSectionEntries() {
		if seen[entry.ID] {
			continue
		}
		out = append(out, entry)
	}

	return out
}

func (m *Model) setDetailWidgetSectionEntries(entries []config.DetailWidgetSection) {
	normalized := normalizeDetailWidgetSectionEntries(entries)
	m.detailWidgetSections = normalized
	m.applyDetailWidgetSectionOverrides()
	m.invalidateDetailCache()
}

func (m Model) detailWidgetSectionConfigEntries() []config.DetailWidgetSection {
	if len(m.detailWidgetSections) == 0 {
		return nil
	}
	out := make([]config.DetailWidgetSection, len(m.detailWidgetSections))
	copy(out, m.detailWidgetSections)
	return out
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

// telemetryUnmappedCategory describes why a telemetry provider id is unmapped.
type telemetryUnmappedCategory string

const (
	telemetryUnmappedUnconfigured        telemetryUnmappedCategory = "unconfigured"
	telemetryUnmappedMappedTargetMissing telemetryUnmappedCategory = "mapped_target_missing"
)

// TelemetryUnmappedDetail is the parsed view of one entry in
// telemetry_unmapped_meta. Suggestion is empty when no candidate target exists.
type TelemetryUnmappedDetail struct {
	Source     string
	Category   telemetryUnmappedCategory
	Suggestion string
}

// telemetryUnmappedDetails aggregates unmapped meta diagnostics across all
// snapshots and returns one detail per source. Sources missing from the meta
// stream (i.e. only present in the legacy CSV) are returned as plain
// "unconfigured" entries with no suggestion.
func (m Model) telemetryUnmappedDetails() []TelemetryUnmappedDetail {
	seen := make(map[string]TelemetryUnmappedDetail)
	for _, snap := range m.snapshots {
		raw := strings.TrimSpace(snap.Diagnostics["telemetry_unmapped_meta"])
		if raw == "" {
			continue
		}
		for _, token := range strings.Split(raw, ",") {
			token = strings.TrimSpace(token)
			if token == "" {
				continue
			}
			eq := strings.IndexByte(token, '=')
			if eq <= 0 {
				continue
			}
			source := strings.TrimSpace(token[:eq])
			rest := strings.TrimSpace(token[eq+1:])
			category := telemetryUnmappedUnconfigured
			suggestion := ""
			colon := strings.IndexByte(rest, ':')
			if colon < 0 {
				category = telemetryUnmappedCategory(rest)
			} else {
				category = telemetryUnmappedCategory(rest[:colon])
				suggestion = strings.TrimSpace(rest[colon+1:])
			}
			if source == "" {
				continue
			}
			seen[source] = TelemetryUnmappedDetail{
				Source:     source,
				Category:   category,
				Suggestion: suggestion,
			}
		}
	}
	for _, source := range m.telemetryUnmappedProviders() {
		if _, ok := seen[source]; !ok {
			seen[source] = TelemetryUnmappedDetail{
				Source:   source,
				Category: telemetryUnmappedUnconfigured,
			}
		}
	}
	keys := core.SortedStringKeys(boolKeys(seen))
	out := make([]TelemetryUnmappedDetail, 0, len(keys))
	for _, k := range keys {
		out = append(out, seen[k])
	}
	return out
}

func boolKeys[V any](m map[string]V) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
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
