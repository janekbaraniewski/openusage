package droid

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type fixedClock struct{ t time.Time }

func (f fixedClock) Now() time.Time { return f.t }

func TestProvider_BasicMetadata(t *testing.T) {
	p := New()
	if p.ID() != ID {
		t.Errorf("ID = %q, want %q", p.ID(), ID)
	}
	if p.Spec().Auth.Type != core.ProviderAuthTypeLocal {
		t.Errorf("auth type = %v, want local", p.Spec().Auth.Type)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "droid", Provider: "droid", Auth: "local"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "missing"))

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v want UNKNOWN", snap.Status)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	dir := t.TempDir()
	uuid1 := "11111111-1111-1111-1111-111111111111"
	uuid2 := "22222222-2222-2222-2222-222222222222"

	json1 := `{
		"model": "custom:Claude-Opus-4.5-Thinking-[Anthropic]-0",
		"providerLock": "anthropic",
		"providerLockTimestamp": "2026-05-18T10:00:00Z",
		"tokenUsage": {
			"inputTokens": 1000,
			"outputTokens": 500,
			"cacheCreationTokens": 50,
			"cacheReadTokens": 200,
			"thinkingTokens": 100
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, uuid1+".settings.json"), []byte(json1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	json2 := `{
		"model": "gemini-2.5-pro",
		"providerLock": "google",
		"providerLockTimestamp": "2026-05-17T15:00:00Z",
		"tokenUsage": {
			"inputTokens": 800,
			"outputTokens": 400
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, uuid2+".settings.json"), []byte(json2), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "droid", Provider: "droid", Auth: "local"}
	acct.SetPath("sessions_dir", dir)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK; msg=%q", snap.Status, snap.Message)
	}

	expect := func(key string, want float64) {
		t.Helper()
		m, ok := snap.Metrics[key]
		if !ok {
			t.Errorf("missing metric %s", key)
			return
		}
		if m.Used == nil || *m.Used != want {
			got := -1.0
			if m.Used != nil {
				got = *m.Used
			}
			t.Errorf("metric %s = %v, want %v", key, got, want)
		}
	}
	expect("total_sessions", 2)
	expect("sessions_today", 1)
	expect("total_input_tokens", 1800)
	expect("total_output_tokens", 900)
	expect("total_reasoning_tokens", 100)
	expect("total_cache_read", 200)
	expect("total_cache_write", 50)

	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := map[string]core.ModelUsageRecord{}
	for _, r := range snap.ModelUsage {
		byModel[r.RawModelID] = r
	}
	// Custom model normalized.
	claude, ok := byModel["claude-opus-4-5-thinking-0"]
	if !ok {
		t.Fatalf("missing normalized claude model; got keys: %v", keysOf(byModel))
	}
	if claude.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("claude provider = %q, want anthropic", claude.Dimensions["upstream_provider"])
	}
	// gemini-2.5-pro normalized to gemini-2-5-pro.
	if _, ok := byModel["gemini-2-5-pro"]; !ok {
		t.Errorf("missing normalized gemini-2-5-pro; got keys: %v", keysOf(byModel))
	}
}

func keysOf(m map[string]core.ModelUsageRecord) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestNormalizeDroidModel(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"custom:Claude-Opus-4.5-Thinking-[Anthropic]-0", "claude-opus-4-5-thinking-0"},
		{"gemini-2.5-pro", "gemini-2-5-pro"},
		{"Claude-Sonnet-4-[Anthropic]", "claude-sonnet-4"},
		{"", ""},
		{"  spaced  ", "spaced"},
		{"already-clean", "already-clean"},
	}
	for _, tc := range cases {
		got := normalizeDroidModel(tc.in)
		if got != tc.want {
			t.Errorf("normalize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestProvider_Fetch_CountsParseErrors(t *testing.T) {
	dir := t.TempDir()
	goodUUID := "33333333-3333-3333-3333-333333333333"
	badUUID := "44444444-4444-4444-4444-444444444444"

	goodJSON := `{
		"model": "gemini-2.5-pro",
		"providerLock": "google",
		"providerLockTimestamp": "2026-05-17T15:00:00Z",
		"tokenUsage": {
			"inputTokens": 800,
			"outputTokens": 400
		}
	}`
	if err := os.WriteFile(filepath.Join(dir, goodUUID+".settings.json"), []byte(goodJSON), 0o600); err != nil {
		t.Fatalf("write good: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, badUUID+".settings.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatalf("write bad: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "droid", Provider: "droid", Auth: "local"}
	acct.SetPath("sessions_dir", dir)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v want OK; msg=%q", snap.Status, snap.Message)
	}
	if got := snap.Diagnostics["parse_errors"]; got != "1" {
		t.Errorf("parse_errors diagnostic = %q, want %q", got, "1")
	}
	if m, ok := snap.Metrics["total_sessions"]; !ok || m.Used == nil || *m.Used != 1 {
		got := -1.0
		if ok && m.Used != nil {
			got = *m.Used
		}
		t.Errorf("total_sessions = %v, want 1 (good file should still surface)", got)
	}
}

func TestParseDroidSession_ZeroTokens(t *testing.T) {
	dir := t.TempDir()
	body := `{
		"model": "claude-3",
		"providerLock": "anthropic",
		"providerLockTimestamp": "2026-05-18T10:00:00Z",
		"tokenUsage": {"inputTokens": 0, "outputTokens": 0}
	}`
	path := filepath.Join(dir, "deadbeef.settings.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	sess, err := parseDroidSession(path)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if sess != nil {
		t.Errorf("got session for zero-token file: %+v", sess)
	}
}
