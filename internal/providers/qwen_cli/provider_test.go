package qwen_cli

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
	if p.Spec().Info.Name != "Qwen CLI" {
		t.Errorf("name = %q", p.Spec().Info.Name)
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "qwen_cli", Provider: "qwen_cli", Auth: "local"}
	acct.SetPath("projects_dir", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("HOME", t.TempDir())

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v want UNKNOWN", snap.Status)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics non-empty: %v", snap.Metrics)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	root := t.TempDir()

	proj1Chats := filepath.Join(root, "proj-aaaa", "chats")
	if err := os.MkdirAll(proj1Chats, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	chat1 := `{"type":"user","timestamp":"2026-01-05T10:00:00.000Z","sessionId":"sess-1","content":"hi"}
{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-05T10:00:01.000Z","sessionId":"sess-1","usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50,"thoughtsTokenCount":20,"cachedContentTokenCount":10}}
{"type":"assistant","model":"qwen3-coder","timestamp":"2026-01-05T10:01:00.000Z","sessionId":"sess-1","usageMetadata":{"promptTokenCount":200,"candidatesTokenCount":80}}
`
	if err := os.WriteFile(filepath.Join(proj1Chats, "session-a.jsonl"), []byte(chat1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	proj2Chats := filepath.Join(root, "proj-bbbb", "chats")
	if err := os.MkdirAll(proj2Chats, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	chat2 := `{"type":"assistant","model":"qwen3-max","timestamp":"2026-01-05T11:00:00.000Z","sessionId":"sess-2","usageMetadata":{"promptTokenCount":500,"candidatesTokenCount":250,"thoughtsTokenCount":0,"cachedContentTokenCount":0}}
`
	if err := os.WriteFile(filepath.Join(proj2Chats, "session-b.jsonl"), []byte(chat2), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// A stray non-chats file should be ignored.
	if err := os.WriteFile(filepath.Join(root, "proj-aaaa", "ignored.jsonl"), []byte(`{}`), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "qwen_cli", Provider: "qwen_cli", Auth: "local"}
	acct.SetPath("projects_dir", root)

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
	expect("total_input_tokens", 800)
	expect("total_output_tokens", 380)
	expect("total_reasoning_tokens", 20)
	expect("total_cache_read", 10)
	expect("total_tokens", 1200)
	expect("sessions_today", 2)
	expect("sessions_7d", 2)

	if _, ok := snap.Metrics["total_cost_usd"]; ok {
		t.Errorf("did not expect total_cost_usd from raw entries")
	}

	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := map[string]core.ModelUsageRecord{}
	for _, r := range snap.ModelUsage {
		byModel[r.RawModelID] = r
	}
	coder, ok := byModel["qwen3-coder"]
	if !ok {
		t.Fatal("missing qwen3-coder")
	}
	if coder.Dimensions["upstream_provider"] != "qwen" {
		t.Errorf("upstream_provider = %q, want qwen", coder.Dimensions["upstream_provider"])
	}
	if coder.Requests == nil || *coder.Requests != 2 {
		t.Errorf("requests = %v, want 2", coder.Requests)
	}
	if coder.RawSource != "jsonl" {
		t.Errorf("raw_source = %q, want jsonl", coder.RawSource)
	}
}

func TestProvider_Fetch_EmptyProjectsDir(t *testing.T) {
	root := t.TempDir()
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "qwen_cli", Provider: "qwen_cli", Auth: "local"}
	acct.SetPath("projects_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v want OK", snap.Status)
	}
	if snap.Message != "No Qwen CLI chats recorded" {
		t.Errorf("message = %q", snap.Message)
	}
}
