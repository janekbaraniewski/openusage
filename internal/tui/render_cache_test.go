package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

const cacheSentinel = "RENDER-CACHE-SENTINEL"

func newRenderCacheModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(0.20, 0.05, config.DashboardConfig{View: config.DashboardViewGrid}, nil, core.TimeWindow30d)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 48})
	m = updated.(Model)
	now := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	snaps := testSnapshots("alpha", "beta")
	for id, s := range snaps {
		s.Timestamp = now
		s.Status = core.StatusOK
		snaps[id] = s
	}
	updated, _ = m.Update(SnapshotsMsg{Snapshots: snaps, TimeWindow: core.TimeWindow30d, RequestID: 1})
	m = updated.(Model)
	m.referenceTime = now.Add(10 * time.Second)
	return m
}

// View() has a value receiver. The detail cache must survive it, so the second
// frame reuses the first frame's render instead of rebuilding the panel.
func TestDetailCacheHitsAfterFirstRender(t *testing.T) {
	m := newRenderCacheModel(t)
	m.mode = modeDetail

	first := m.View()
	if m.detailCache.key == "" {
		t.Fatal("first detail render did not populate the shared cache")
	}
	// Swap the cached body: a cache hit renders the sentinel, a miss
	// re-renders the real panel.
	m.detailCache.content = cacheSentinel
	if got := m.View(); !strings.Contains(got, cacheSentinel) {
		t.Fatalf("second detail render missed the cache:\n%s", got)
	}

	// A new wall-clock second changes the "X ago" labels and must miss.
	m.referenceTime = m.referenceTime.Add(time.Second)
	if got := m.View(); strings.Contains(got, cacheSentinel) {
		t.Fatal("detail cache served a frame from a previous second")
	}
	_ = first
}

func TestDetailCacheInvalidatedOnResizeAndData(t *testing.T) {
	m := newRenderCacheModel(t)
	m.mode = modeDetail
	_ = m.View()

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 150, Height: 48})
	m = updated.(Model)
	if m.detailCache.key != "" {
		t.Fatal("resize did not invalidate the detail cache")
	}

	_ = m.View()
	updated, _ = m.Update(SnapshotsMsg{Snapshots: m.snapshots, TimeWindow: core.TimeWindow30d, RequestID: 2})
	m = updated.(Model)
	if m.detailCache.key != "" {
		t.Fatal("new snapshots did not invalidate the detail cache")
	}
}

func TestAnalyticsCacheHitsAfterFirstRender(t *testing.T) {
	m := newRenderCacheModel(t)
	m.screen = screenAnalytics

	_ = m.View()
	if m.analyticsCache.key == "" {
		t.Fatal("first analytics render did not populate the shared cache")
	}
	m.analyticsCache.hasData = true
	m.analyticsCache.content = cacheSentinel
	if got := m.View(); !strings.Contains(got, cacheSentinel) {
		t.Fatalf("second analytics render missed the cache:\n%s", got)
	}

	// Disabling a provider in settings changes the visible set and must miss.
	m.providerEnabled["beta"] = false
	m.rebuildSortedIDs()
	if got := m.View(); strings.Contains(got, cacheSentinel) {
		t.Fatal("analytics cache ignored a provider toggle")
	}
}

// The settings modal replaces the frame, so View() must not render the
// dashboard (detail panel included) underneath it.
func TestSettingsOpenSkipsDashboardRender(t *testing.T) {
	m := newRenderCacheModel(t)
	m.mode = modeDetail
	m.settings.show = true

	out := m.View()
	if strings.TrimSpace(out) == "" {
		t.Fatal("settings modal rendered empty")
	}
	if m.detailCache.key != "" {
		t.Fatal("View() rendered the detail panel behind the settings modal")
	}
}
