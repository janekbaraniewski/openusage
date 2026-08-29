package tui

import (
	"github.com/janekbaraniewski/openusage/internal/config"
)

type dashboardViewMode string

const (
	dashboardViewSplit dashboardViewMode = dashboardViewMode(config.DashboardViewSplit)
)

type dashboardViewOption struct {
	ID          dashboardViewMode
	Label       string
	Description string
}

var dashboardViewOptions = []dashboardViewOption{
	{
		ID:          dashboardViewSplit,
		Label:       "Split",
		Description: "Navigator pane on the left, focus pane on the right.",
	},
}

func normalizeDashboardViewMode(raw string) dashboardViewMode {
	return dashboardViewSplit
}

func dashboardViewLabel(mode dashboardViewMode) string {
	return "Split"
}

func dashboardViewIndex(mode dashboardViewMode) int {
	return 0
}

func dashboardViewByIndex(index int) dashboardViewMode {
	return dashboardViewSplit
}

func (m Model) configuredDashboardView() dashboardViewMode {
	return dashboardViewSplit
}

func (m Model) activeDashboardView() dashboardViewMode {
	return dashboardViewSplit
}

func (m Model) dashboardViewStatusLabel() string {
	return "Split"
}

func (m *Model) setDashboardView(mode dashboardViewMode) {
	m.dashboardView = dashboardViewSplit
	m.mode = modeList
	m.detailOffset = 0
	m.detailTab = 0
	m.tileOffset = 0
	m.invalidateTileBodyCache()
	m.invalidateDetailCache()
}

func (m Model) nextDashboardView(step int) dashboardViewMode {
	return dashboardViewSplit
}
