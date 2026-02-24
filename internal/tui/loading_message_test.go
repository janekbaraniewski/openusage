package tui

import "testing"

func TestResolveLoadingMessage_PrefersOwnMessage(t *testing.T) {
	m := Model{}
	got := m.resolveLoadingMessage("Gemini CLI (user@host)", "Syncing telemetry...")
	if got != "Gemini CLI (user@host)" {
		t.Fatalf("resolveLoadingMessage() = %q, want own message", got)
	}
}

func TestResolveLoadingMessage_UsesProvidedFallback(t *testing.T) {
	m := Model{}
	got := m.resolveLoadingMessage("", "Syncing telemetry...")
	if got != "Syncing telemetry..." {
		t.Fatalf("resolveLoadingMessage() = %q, want fallback", got)
	}
}

func TestResolveLoadingMessage_IgnoresConnectedPseudoMessage(t *testing.T) {
	m := Model{}
	got := m.resolveLoadingMessage("connected", "Syncing telemetry...")
	if got != "Syncing telemetry..." {
		t.Fatalf("resolveLoadingMessage() = %q, want fallback for connected", got)
	}
}
