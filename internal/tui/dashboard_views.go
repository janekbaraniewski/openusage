package tui

import (
	"strings"

	"github.com/janekbaraniewski/openusage/internal/config"
)

type dashboardViewMode string

const (
	dashboardViewGrid    dashboardViewMode = dashboardViewMode(config.DashboardViewGrid)
	dashboardViewStacked dashboardViewMode = dashboardViewMode(config.DashboardViewStacked)
	dashboardViewTabs    dashboardViewMode = dashboardViewMode(config.DashboardViewTabs)
	dashboardViewSplit   dashboardViewMode = dashboardViewMode(config.DashboardViewSplit)
	dashboardViewCompare dashboardViewMode = dashboardViewMode(config.DashboardViewCompare)
)

type dashboardViewOption struct {
	ID          dashboardViewMode
	Label       string
	Description string
}

var dashboardViewOptions = []dashboardViewOption{
	{
		ID:          dashboardViewGrid,
		Label:       "Grid",
		Description: "Adaptive multi-column layout with per-tile summaries.",
	},
	{
		ID:          dashboardViewStacked,
		Label:       "Stacked",
		Description: "Full widgets in one scrollable column.",
	},
	{
		ID:          dashboardViewTabs,
		Label:       "Tabs",
		Description: "Full-height focus pane with visible tab strip.",
	},
	{
		ID:          dashboardViewSplit,
		Label:       "Split",
		Description: "Navigator pane on the left, focus pane on the right.",
	},
	{
		ID:          dashboardViewCompare,
		Label:       "Compare",
		Description: "Side-by-side panes for active and neighboring provider.",
	},
}

func normalizeDashboardViewMode(raw string) dashboardViewMode {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(dashboardViewGrid):
		return dashboardViewGrid
	case string(dashboardViewStacked):
		return dashboardViewStacked
	case string(dashboardViewTabs):
		return dashboardViewTabs
	case string(dashboardViewSplit):
		return dashboardViewSplit
	case string(dashboardViewCompare):
		return dashboardViewCompare
	case config.DashboardViewList:
		return dashboardViewSplit
	default:
		return dashboardViewGrid
	}
}

func dashboardViewLabel(mode dashboardViewMode) string {
	for _, option := range dashboardViewOptions {
		if option.ID == mode {
			return option.Label
		}
	}
	return dashboardViewOptions[0].Label
}

func dashboardViewIndex(mode dashboardViewMode) int {
	for i, option := range dashboardViewOptions {
		if option.ID == mode {
			return i
		}
	}
	return 0
}

func dashboardViewByIndex(index int) dashboardViewMode {
	if len(dashboardViewOptions) == 0 {
		return dashboardViewGrid
	}
	if index < 0 {
		index = 0
	}
	if index >= len(dashboardViewOptions) {
		index = len(dashboardViewOptions) - 1
	}
	return dashboardViewOptions[index].ID
}

func minTwoColumnDashboardWidth() int {
	return 2*(tileMinMultiColumnWidth+tileBorderH) + tileGapH + 2
}

func (m Model) configuredDashboardView() dashboardViewMode {
	return normalizeDashboardViewMode(string(m.dashboardView))
}

func (m Model) shouldForceStackedDashboardView() bool {
	if m.width <= 0 {
		return false
	}
	if len(m.filteredIDs()) <= 1 {
		return false
	}
	return m.width < minTwoColumnDashboardWidth()
}

func (m Model) activeDashboardView() dashboardViewMode {
	if m.shouldForceStackedDashboardView() {
		return dashboardViewStacked
	}
	return m.configuredDashboardView()
}

func (m Model) dashboardViewStatusLabel() string {
	active := m.activeDashboardView()
	configured := m.configuredDashboardView()
	if active != configured {
		return dashboardViewLabel(active) + " (auto)"
	}
	return dashboardViewLabel(active)
}

func (m *Model) setDashboardView(mode dashboardViewMode) {
	m.dashboardView = normalizeDashboardViewMode(string(mode))
	m.mode = modeList
	m.detailOffset = 0
	m.detailTab = 0
	m.tileOffset = 0
}

func (m Model) nextDashboardView(step int) dashboardViewMode {
	total := len(dashboardViewOptions)
	if total == 0 {
		return dashboardViewGrid
	}
	idx := dashboardViewIndex(m.configuredDashboardView())
	next := (idx + step) % total
	if next < 0 {
		next += total
	}
	return dashboardViewByIndex(next)
}
