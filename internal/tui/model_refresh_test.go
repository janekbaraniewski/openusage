package tui

import (
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRequestRefreshInvokesCallback(t *testing.T) {
	m := Model{}
	m.timeWindow = core.TimeWindow7d

	refreshCalls := 0
	var gotWindow core.TimeWindow
	m.SetOnRefresh(func(window core.TimeWindow) {
		refreshCalls++
		gotWindow = window
	})

	updated := m.requestRefresh()
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh callback calls = %d, want 1", refreshCalls)
	}
	if gotWindow != core.TimeWindow7d {
		t.Fatalf("refresh callback window = %q, want %q", gotWindow, core.TimeWindow7d)
	}
}
