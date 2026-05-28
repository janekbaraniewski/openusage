package codebuff

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
	if p.Spec().Info.Name != "Codebuff" {
		t.Errorf("name = %q", p.Spec().Info.Name)
	}
	if p.Spec().Info.DocURL != "https://codebuff.com/" {
		t.Errorf("doc url = %q", p.Spec().Info.DocURL)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEBUFF_DATA_DIR", "")

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "codebuff", Provider: "codebuff", Auth: "local"}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v want UNKNOWN", snap.Status)
	}
}

func TestProvider_Fetch_MultiChannel(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEBUFF_DATA_DIR", "")

	bodyClaude := `[
		{"role":"user","id":"u1"},
		{"role":"assistant","id":"a1","metadata":{"usage":{
			"input_tokens":1000,"output_tokens":500,
			"cache_read_input_tokens":200,"cache_creation_input_tokens":50,
			"credits": 0.5,"model":"claude-opus-4-7"
		},"timestamp":"2026-05-18T10:00:00.000Z"}}
	]`
	bodyGPT := `[
		{"role":"assistant","id":"b1","metadata":{"usage":{
			"input_tokens":800,"output_tokens":400,"model":"gpt-5"
		},"timestamp":"2026-05-18T11:00:00.000Z"}}
	]`
	mkChat := func(channel, project, chatID, body string) {
		dir := filepath.Join(root, channel, "projects", project, "chats", chatID)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(dir, "chat-messages.json"), []byte(body), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	mkChat("manicode", "proj-a", "2026-05-18T10-00-00.000Z", bodyClaude)
	mkChat("manicode-dev", "proj-b", "2026-05-18T11-00-00.000Z", bodyGPT)

	// Point the account at the synthetic root for the first channel and use
	// CODEBUFF_DATA_DIR for the second.
	t.Setenv("CODEBUFF_DATA_DIR", filepath.Join(root, "manicode-dev"))

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "codebuff", Provider: "codebuff", Auth: "local"}
	acct.SetPath("data_dir", filepath.Join(root, "manicode"))

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
	expect("total_chats", 2)
	expect("total_input_tokens", 1800)
	expect("total_output_tokens", 900)
	expect("total_cache_read", 200)
	expect("total_cache_write", 50)
	expect("total_credits", 0.5)
	expect("total_messages", 2)

	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := map[string]core.ModelUsageRecord{}
	for _, r := range snap.ModelUsage {
		byModel[r.RawModelID] = r
	}
	claude, ok := byModel["claude-opus-4-7"]
	if !ok {
		t.Fatal("missing claude-opus-4-7")
	}
	if claude.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("claude upstream = %q", claude.Dimensions["upstream_provider"])
	}
	if claude.Requests == nil || *claude.Requests != 1 {
		t.Errorf("claude requests = %v", claude.Requests)
	}

	// today-relative chats: poll time is 2026-05-18 → both chats today.
	expect("chats_today", 2)
	expect("chats_7d", 2)
}

func TestProvider_HasChanged_NoDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEBUFF_DATA_DIR", "")
	p := New()
	changed, err := p.HasChanged(core.AccountConfig{}, time.Now())
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if changed {
		t.Error("expected changed=false when no dir")
	}
}
