package tui

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

var ansiPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// StripANSI removes terminal escape sequences from rendered TUI output.
func StripANSI(s string) string {
	return ansiPattern.ReplaceAllString(s, "")
}

// WebProjector renders TUI-equivalent account views for the web dashboard.
type WebProjector struct {
	TimeWindow             core.TimeWindow
	WarnThreshold          float64
	CritThreshold          float64
	UsageMode              string
	HideCostsGlobal        *bool
	HideCostsByAccount     map[string]*bool
	HideSectionsWithNoData bool
	DashboardCfg           config.DashboardConfig
	Accounts               []core.AccountConfig
	TileWidth              int
	DetailWidth            int
	Now                    time.Time
}

// WebAccountView is the structured payload for one provider account in the browser UI.
type WebAccountView struct {
	Key            string            `json:"key"`
	ProviderID     string            `json:"provider_id"`
	ProviderName   string            `json:"provider_name"`
	AccountID      string            `json:"account_id"`
	Status         string            `json:"status"`
	StatusBadge    string            `json:"status_badge"`
	StatusIcon     string            `json:"status_icon"`
	AccentColor    string            `json:"accent_color"`
	Summary        string            `json:"summary"`
	Detail         string            `json:"detail,omitempty"`
	TagEmoji       string            `json:"tag_emoji,omitempty"`
	TagLabel       string            `json:"tag_label,omitempty"`
	GaugePercent   float64           `json:"gauge_percent,omitempty"`
	Message        string            `json:"message,omitempty"`
	Timestamp      time.Time         `json:"timestamp"`
	TileLines      []string          `json:"tile_lines"`
	DetailSections []WebDetailSection `json:"detail_sections"`
	Resets         []WebResetPill    `json:"resets,omitempty"`
	DailyCost      []core.TimePoint  `json:"daily_cost,omitempty"`
}

type WebDetailSection struct {
	Title string   `json:"title"`
	Icon  string   `json:"icon,omitempty"`
	Lines []string `json:"lines"`
}

type WebResetPill struct {
	Label    string `json:"label"`
	Duration string `json:"duration"`
	Urgent   bool   `json:"urgent,omitempty"`
}

// WebThemeTokens exposes the active palette for CSS variable injection.
type WebThemeTokens struct {
	Name string `json:"name"`
	Icon string `json:"icon,omitempty"`
	Base string `json:"base"`
	Mantle string `json:"mantle"`
	Surface0 string `json:"surface0"`
	Surface1 string `json:"surface1"`
	Surface2 string `json:"surface2"`
	Text string `json:"text"`
	Subtext string `json:"subtext"`
	Dim string `json:"dim"`
	Accent string `json:"accent"`
	Blue string `json:"blue"`
	Sapphire string `json:"sapphire"`
	Green string `json:"green"`
	Yellow string `json:"yellow"`
	Red string `json:"red"`
	Peach string `json:"peach"`
	Teal string `json:"teal"`
	Lavender string `json:"lavender"`
	Mauve string `json:"mauve"`
}

func NewWebProjectorFromConfig(cfg config.Config) WebProjector {
	usageMode := cfg.Dashboard.UsageMode
	if usageMode == "" {
		usageMode = config.UsageModeRemaining
	}
	hideByAccount := make(map[string]*bool, len(cfg.Dashboard.Providers))
	for _, p := range cfg.Dashboard.Providers {
		hideByAccount[p.AccountID] = p.HideCosts
	}
	accounts := core.MergeAccounts(cfg.Accounts, cfg.AutoDetectedAccounts)
	return WebProjector{
		TimeWindow:             core.ParseTimeWindow(cfg.Data.TimeWindow),
		WarnThreshold:          cfg.UI.WarnThreshold,
		CritThreshold:          cfg.UI.CritThreshold,
		UsageMode:              usageMode,
		HideCostsGlobal:        cfg.Dashboard.HideCosts,
		HideCostsByAccount:     hideByAccount,
		HideSectionsWithNoData: cfg.Dashboard.HideSectionsWithNoData,
		DashboardCfg:           cfg.Dashboard,
		Accounts:               accounts,
		TileWidth:              76,
		DetailWidth:            88,
		Now:                    time.Now(),
	}
}

// OrderSnapshots returns dashboard-visible snapshots in the same order as the TUI.
func (p WebProjector) OrderSnapshots(snaps map[string]core.UsageSnapshot) []core.UsageSnapshot {
	if len(snaps) == 0 {
		return nil
	}
	m := p.model()
	m.snapshots = snaps
	m.rebuildSortedIDs()
	out := make([]core.UsageSnapshot, 0, len(m.sortedIDs))
	for _, id := range m.sortedIDs {
		if snap, ok := snaps[id]; ok {
			out = append(out, snap)
		}
	}
	return out
}

// SnapshotsToMap indexes snapshots by account ID, matching the TUI snapshot map.
func SnapshotsToMap(snaps []core.UsageSnapshot) map[string]core.UsageSnapshot {
	out := make(map[string]core.UsageSnapshot, len(snaps))
	for _, snap := range snaps {
		id := strings.TrimSpace(snap.AccountID)
		if id == "" {
			continue
		}
		out[id] = snap
	}
	return out
}

func (p WebProjector) ProjectSnapshots(snaps []core.UsageSnapshot, names map[string]string) []WebAccountView {
	out := make([]WebAccountView, 0, len(snaps))
	for _, snap := range snaps {
		name := names[snap.ProviderID]
		if name == "" {
			name = snap.ProviderID
		}
		out = append(out, p.ProjectSnapshot(snap, name))
	}
	return out
}

func (p WebProjector) ProjectSnapshot(snap core.UsageSnapshot, providerName string) WebAccountView {
	m := p.model()
	widget := dashboardWidget(snap.ProviderID)
	hideCosts := m.resolveHideCosts(snap)
	di := computeDisplayInfo(snap, widget, hideCosts, m.usageMode)

	tileW := p.TileWidth
	if tileW < 40 {
		tileW = 40
	}
	tile := m.renderTile(snap, true, false, tileW, 0, 0)
	tileLines := strings.Split(StripANSI(tile), "\n")

	detailW := p.DetailWidth
	if detailW < 40 {
		detailW = 40
	}
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	detailSections := projectDetailSections(snap, widget, detailW, m.warnThreshold, m.critThreshold, m.timeWindow, hideCosts, now, m.usageMode)

	view := WebAccountView{
		Key:            fmt.Sprintf("%s:%s", snap.ProviderID, snap.AccountID),
		ProviderID:     snap.ProviderID,
		ProviderName:   providerName,
		AccountID:      snap.AccountID,
		Status:         string(snap.Status),
		StatusBadge:    StripANSI(SnapshotStatusBadge(snap)),
		StatusIcon:     StatusIcon(snap.Status),
		AccentColor:    colorHex(ProviderColor(snap.ProviderID)),
		Summary:        di.summary,
		Detail:         di.detail,
		TagEmoji:       di.tagEmoji,
		TagLabel:       di.tagLabel,
		Message:        snap.Message,
		Timestamp:      snap.Timestamp,
		TileLines:      tileLines,
		DetailSections: detailSections,
		Resets:         projectResetPills(snap, widget, now),
	}
	if di.gaugePercent >= 0 {
		view.GaugePercent = di.gaugePercent
	}
	if cost := snap.DailySeries["cost"]; len(cost) > 0 {
		view.DailyCost = cost
	} else if cost := snap.DailySeries["analytics_cost"]; len(cost) > 0 {
		view.DailyCost = cost
	}
	return view
}

func projectDetailSections(
	snap core.UsageSnapshot,
	widget core.DashboardWidget,
	w int,
	warnThresh, critThresh float64,
	timeWindow core.TimeWindow,
	hideCosts bool,
	now time.Time,
	usageMode string,
) []WebDetailSection {
	sections := buildDetailSections(snap, widget, w, warnThresh, critThresh, timeWindow, hideCosts, now, usageMode)
	out := make([]WebDetailSection, 0, len(sections))
	for _, sec := range sections {
		lines := make([]string, 0, len(sec.lines))
		for _, line := range sec.lines {
			plain := strings.TrimRight(StripANSI(line), " ")
			if plain == "" {
				lines = append(lines, "")
				continue
			}
			lines = append(lines, plain)
		}
		out = append(out, WebDetailSection{
			Title: sec.title,
			Icon:  sec.icon,
			Lines: lines,
		})
	}
	return out
}

func projectResetPills(snap core.UsageSnapshot, widget core.DashboardWidget, now time.Time) []WebResetPill {
	entries := collectActiveResetEntries(snap, widget)
	if len(entries) == 0 {
		return nil
	}
	out := make([]WebResetPill, 0, len(entries))
	for _, e := range entries {
		dur := e.at.Sub(now)
		if dur <= 0 {
			continue
		}
		pill := WebResetPill{
			Label:    e.label,
			Duration: formatHeaderDuration(dur),
		}
		if dur < 10*time.Minute {
			pill.Urgent = true
		}
		out = append(out, pill)
	}
	return out
}

func (p WebProjector) model() Model {
	now := p.Now
	if now.IsZero() {
		now = time.Now()
	}
	warn := p.WarnThreshold
	if warn <= 0 {
		warn = 0.25
	}
	crit := p.CritThreshold
	if crit <= 0 {
		crit = 0.1
	}
	usageMode := p.UsageMode
	if usageMode == "" {
		usageMode = config.UsageModeRemaining
	}
	tw := p.TimeWindow
	if tw == "" {
		tw = core.TimeWindow30d
	}
	m := Model{
		warnThreshold:          warn,
		critThreshold:          crit,
		usageMode:              usageMode,
		hideCostsGlobal:        p.HideCostsGlobal,
		hideCostsByAccount:     p.HideCostsByAccount,
		hideSectionsWithNoData: p.HideSectionsWithNoData,
		timeWindow:             tw,
		hasData:                true,
		referenceTime:          now,
		snapshots:              make(map[string]core.UsageSnapshot),
		providerEnabled:        make(map[string]bool),
		accountProviders:       make(map[string]string),
	}
	if len(p.Accounts) > 0 || len(p.DashboardCfg.Providers) > 0 || p.DashboardCfg.UsageMode != "" {
		m.applyDashboardConfig(p.DashboardCfg, p.Accounts)
	}
	return m
}

func ThemeTokensForName(name string) WebThemeTokens {
	_ = SetThemeByName(name)
	t := ActiveTheme()
	return WebThemeTokensFromTheme(t)
}

func WebThemeTokensFromTheme(t Theme) WebThemeTokens {
	return WebThemeTokens{
		Name:     t.Name,
		Icon:     t.Icon,
		Base:     colorHex(t.Base),
		Mantle:   colorHex(t.Mantle),
		Surface0: colorHex(t.Surface0),
		Surface1: colorHex(t.Surface1),
		Surface2: colorHex(t.Surface2),
		Text:     colorHex(t.Text),
		Subtext:  colorHex(t.Subtext),
		Dim:      colorHex(t.Dim),
		Accent:   colorHex(t.Accent),
		Blue:     colorHex(t.Blue),
		Sapphire: colorHex(t.Sapphire),
		Green:    colorHex(t.Green),
		Yellow:   colorHex(t.Yellow),
		Red:      colorHex(t.Red),
		Peach:    colorHex(t.Peach),
		Teal:     colorHex(t.Teal),
		Lavender: colorHex(t.Lavender),
		Mauve:    colorHex(t.Mauve),
	}
}

func colorHex(c lipgloss.Color) string {
	return strings.TrimSpace(string(c))
}
