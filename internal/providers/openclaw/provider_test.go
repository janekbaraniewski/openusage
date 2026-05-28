package openclaw

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
	if p.Spec().Info.Name != "OpenClaw" {
		t.Errorf("name = %q, want OpenClaw", p.Spec().Info.Name)
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "openclaw", Provider: "openclaw", Auth: "local"}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("status = %v, want UNKNOWN", snap.Status)
	}
	if len(snap.Metrics) != 0 {
		t.Errorf("metrics non-empty: %v", snap.Metrics)
	}
}

func TestProvider_Fetch_HappyPath_WithIndex(t *testing.T) {
	root := t.TempDir()
	agents := filepath.Join(root, "agents")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// One transcript with two assistant turns and vendor cost.
	transcript := `{"type":"model","data":{"provider":"anthropic","modelId":"claude-opus-4-5"}}
{"type":"message","message":{"role":"user","timestamp":1735689600000,"usage":{"input":50,"output":0}}}
{"type":"message","message":{"role":"assistant","timestamp":1735689601000,"usage":{"input":100,"output":50,"cacheRead":10,"cacheWrite":5,"cost":{"total":0.0042}}}}
{"type":"message","message":{"role":"assistant","timestamp":1735689602000,"provider":"anthropic","model":"claude-opus-4-5","usage":{"input":200,"output":100,"cost":{"total":0.0084}}}}
`
	transcriptPath := filepath.Join(agents, "ses_abc123.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcript), 0o600); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	index := `{
		"k1": {"sessionId": "ses_abc123", "sessionFile": "ses_abc123.jsonl"}
	}`
	if err := os.WriteFile(filepath.Join(agents, "sessions.json"), []byte(index), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "openclaw", Provider: "openclaw", Auth: "local"}
	acct.SetPath("agents_dir", agents)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, want OK; msg=%q", snap.Status, snap.Message)
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
	expect("total_sessions", 1)
	expect("total_input_tokens", 300)
	expect("total_output_tokens", 150)
	expect("total_cache_read", 10)
	expect("total_cache_write", 5)
	expect("total_cost_usd", 0.0126)

	if len(snap.ModelUsage) != 1 {
		t.Fatalf("len(ModelUsage) = %d, want 1", len(snap.ModelUsage))
	}
	rec := snap.ModelUsage[0]
	if rec.RawModelID != "claude-opus-4-5" {
		t.Errorf("model = %q", rec.RawModelID)
	}
	if rec.Dimensions["upstream_provider"] != "anthropic" {
		t.Errorf("upstream_provider = %q", rec.Dimensions["upstream_provider"])
	}
	if rec.Requests == nil || *rec.Requests != 2 {
		t.Errorf("requests = %v, want 2", rec.Requests)
	}
}

func TestProvider_Fetch_FlatScan_NoIndex(t *testing.T) {
	agents := t.TempDir()

	a := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"anthropic","model":"claude-opus-4-5","usage":{"input":100,"output":50}}}` + "\n"
	b := `{"type":"message","message":{"role":"assistant","timestamp":1735689600000,"provider":"openai","model":"gpt-4o","usage":{"input":200,"output":100}}}` + "\n"
	if err := os.WriteFile(filepath.Join(agents, "ses_a.jsonl"), []byte(a), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agents, "ses_b.jsonl"), []byte(b), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agents, "ignored.txt"), []byte("noise"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "openclaw", Provider: "openclaw", Auth: "local"}
	acct.SetPath("agents_dir", agents)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v", snap.Status)
	}
	m := snap.Metrics["total_sessions"]
	if m.Used == nil || *m.Used != 2 {
		t.Errorf("sessions = %v, want 2", m.Used)
	}
	if len(snap.ModelUsage) != 2 {
		t.Errorf("models = %d, want 2", len(snap.ModelUsage))
	}
}
