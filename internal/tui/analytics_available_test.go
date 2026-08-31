package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func newAnalyticsModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(0.20, 0.05, config.DashboardConfig{View: config.DashboardViewGrid}, nil, core.TimeWindow("30d"))
	m.width, m.height = 140, 44
	// hasData true with no snapshots is the real first-run case: the daemon is
	// reachable but nothing has been collected yet. With hasData false View()
	// returns the splash and never reaches the analytics renderer.
	m.hasData = true
	return m
}

// Analytics is no longer gated, so Tab must always reach it.
func TestAnalyticsAlwaysAvailable(t *testing.T) {
	m := newAnalyticsModel(t)

	screens := m.availableScreens()
	if len(screens) != 2 || screens[0] != screenDashboard || screens[1] != screenAnalytics {
		t.Fatalf("availableScreens() = %v, want [dashboard analytics]", screens)
	}

	if got := m.nextScreen(1); got != screenAnalytics {
		t.Errorf("nextScreen(1) from dashboard = %v, want analytics", got)
	}
	m.screen = screenAnalytics
	if got := m.nextScreen(1); got != screenDashboard {
		t.Errorf("nextScreen(1) from analytics = %v, want to wrap to dashboard", got)
	}
}

// The screen is reachable before any data exists, so the empty state has to
// render rather than panic or come back blank.
func TestAnalyticsScreenRendersWithNoData(t *testing.T) {
	m := newAnalyticsModel(t)
	m.screen = screenAnalytics

	out := m.View()
	if strings.TrimSpace(out) == "" {
		t.Fatal("analytics view rendered empty with no snapshots")
	}
}

// A first-run terminal can be narrow, and the screen is reachable immediately.
func TestAnalyticsScreenRendersAcrossWidths(t *testing.T) {
	for _, size := range [][2]int{{60, 20}, {80, 24}, {140, 44}, {200, 60}} {
		m := newAnalyticsModel(t)
		m.width, m.height = size[0], size[1]
		m.screen = screenAnalytics
		m.analyticsCache = analyticsRenderCacheEntry{}
		if strings.TrimSpace(m.View()) == "" {
			t.Errorf("analytics view empty at %dx%d", size[0], size[1])
		}
	}
}

// Analytics costs nothing while another screen is active: the render path is
// behind the screen switch. This pins that the dashboard render never builds
// the analytics aggregate, which is why no opt-out is needed.
func TestDashboardRenderDoesNotBuildAnalyticsCache(t *testing.T) {
	m := newAnalyticsModel(t)
	m.screen = screenDashboard
	m.analyticsCache = analyticsRenderCacheEntry{}

	_ = m.View()

	if m.analyticsCache.key != "" {
		t.Errorf("dashboard render populated the analytics cache (key=%q); analytics work should be lazy", m.analyticsCache.key)
	}
}
