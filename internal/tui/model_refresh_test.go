package tui

import "testing"

func TestRequestRefreshInvokesCallback(t *testing.T) {
	m := Model{}

	refreshCalls := 0
	m.SetOnRefresh(func() {
		refreshCalls++
	})

	updated := m.requestRefresh()
	if !updated.refreshing {
		t.Fatal("refreshing = false, want true")
	}
	if refreshCalls != 1 {
		t.Fatalf("refresh callback calls = %d, want 1", refreshCalls)
	}
}
