package pi

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
	if p.Spec().Info.Name != "Pi" {
		t.Errorf("name = %q, want Pi", p.Spec().Info.Name)
	}
	if p.Spec().Info.DocURL == "" {
		t.Error("DocURL is empty")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "pi", Provider: "pi", Auth: "local"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "missing"))

	// resolveSessionsDirs will fall back to defaults; on most test machines
	// neither default exists, so we'll get Unknown. We tolerate either
	// Unknown (no defaults) or OK (defaults exist) but require no metrics
	// in the Unknown case.
	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status == core.StatusUnknown && len(snap.Metrics) != 0 {
		t.Errorf("Unknown status but metrics non-empty: %v", snap.Metrics)
	}
}

func TestProvider_Fetch_HappyPath(t *testing.T) {
	root := t.TempDir()
	wsDir := filepath.Join(root, "-home-jane-work-project-x")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	session1 := `{"type":"session","id":"pi_ses_001","timestamp":"2026-01-02T10:00:00.000Z","cwd":"/home/jane/work/project-x"}
{"type":"message","timestamp":"2026-01-02T10:00:01.000Z","message":{"role":"assistant","model":"claude-3-5-sonnet","provider":"anthropic","usage":{"input":1000,"output":500,"cacheRead":100,"cacheWrite":50,"cost":{"input":0.125,"output":0.125,"cacheRead":0,"cacheWrite":0,"total":0.25}}}}
{"type":"message","timestamp":"2026-01-02T10:00:02.000Z","message":{"role":"assistant","model":"gpt-4o","provider":"openai","usage":{"input":800,"output":400}}}
{"type":"message","timestamp":"2026-01-02T10:00:03.000Z","message":{"role":"user","model":"x"}}
malformed-line
`
	if err := os.WriteFile(filepath.Join(wsDir, "session-001.jsonl"), []byte(session1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	session2 := `{"type":"session","id":"pi_ses_002","timestamp":"2026-01-02T11:00:00.000Z","cwd":"/home/jane/work/project-x"}
{"type":"message","timestamp":"2026-01-02T11:00:01.000Z","message":{"role":"assistant","model":"claude-3-5-sonnet","provider":"anthropic","usage":{"input":2000,"output":1000,"cost":{"total":0.5}}}}
`
	if err := os.WriteFile(filepath.Join(wsDir, "session-002.jsonl"), []byte(session2), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Non-jsonl file should be ignored.
	if err := os.WriteFile(filepath.Join(wsDir, "README"), []byte("ignore"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "pi", Provider: "pi", Auth: "local"}
	acct.SetPath("sessions_dir", root)

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
	expect("sessions_today", 2)
	expect("sessions_7d", 2)
	// claude: 1000+2000 = 3000; gpt: 800
	expect("total_input_tokens", 3800)
	// claude: 500+1000 = 1500; gpt: 400
	expect("total_output_tokens", 1900)
	expect("total_tokens", 5700)
	expect("total_cache_read", 100)
	expect("total_cache_write", 50)
	// Pi's own usage.cost.total: claude 0.25 + 0.5; gpt has no cost block.
	expect("total_cost_usd", 0.75)

	if len(snap.ModelUsage) != 2 {
		t.Fatalf("len(ModelUsage) = %d, want 2", len(snap.ModelUsage))
	}
	byModel := map[string]core.ModelUsageRecord{}
	for _, r := range snap.ModelUsage {
		byModel[r.RawModelID] = r
	}
	claude, ok := byModel["claude-3-5-sonnet"]
	if !ok {
		t.Fatal("missing claude-3-5-sonnet")
	}
	if claude.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("claude upstream = %q, want anthropic", claude.Dimensions["upstream_provider"])
	}
	if claude.Dimensions["workspace_label"] != "project-x" {
		t.Errorf("claude workspace = %q, want project-x", claude.Dimensions["workspace_label"])
	}
	if claude.Requests == nil || *claude.Requests != 2 {
		t.Errorf("claude requests = %v, want 2", claude.Requests)
	}
	if claude.CostUSD == nil || *claude.CostUSD != 0.75 {
		t.Errorf("claude cost = %v, want 0.75", claude.CostUSD)
	}
	if gpt := byModel["gpt-4o"]; gpt.CostUSD != nil {
		t.Errorf("gpt-4o cost = %v, want nil (no cost block)", *gpt.CostUSD)
	}
	if claude.RawSource != "jsonl" {
		t.Errorf("claude raw_source = %q, want jsonl", claude.RawSource)
	}

	if _, ok := snap.DailySeries["sessions"]; !ok {
		t.Error("missing sessions DailySeries")
	}
	if _, ok := snap.DailySeries["tokens"]; !ok {
		t.Error("missing tokens DailySeries")
	}
	if _, ok := snap.DailySeries["cost"]; !ok {
		t.Error("missing cost DailySeries")
	}
}

func TestProvider_Fetch_EmptyDir(t *testing.T) {
	root := t.TempDir()
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "pi", Provider: "pi", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v, want OK", snap.Status)
	}
	if snap.Message == "" {
		t.Error("expected non-empty message")
	}
}

func TestProvider_HasChanged(t *testing.T) {
	root := t.TempDir()
	wsDir := filepath.Join(root, "ws")
	if err := os.MkdirAll(wsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	path := filepath.Join(wsDir, "s.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","id":"x","cwd":"/x"}`+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := New()
	acct := core.AccountConfig{ID: "pi", Provider: "pi", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	changed, err := p.HasChanged(acct, time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if !changed {
		t.Error("expected HasChanged=true for recent file")
	}

	changed, err = p.HasChanged(acct, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("HasChanged: %v", err)
	}
	if changed {
		t.Error("expected HasChanged=false for future cutoff")
	}
}
