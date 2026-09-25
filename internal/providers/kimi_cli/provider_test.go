package kimi_cli

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
	if p.Spec().Info.Name != "Kimi CLI" {
		t.Errorf("name = %q, want %q", p.Spec().Info.Name, "Kimi CLI")
	}
	if p.DashboardWidget().IsZero() {
		t.Error("DashboardWidget is zero")
	}
}

func TestProvider_Fetch_MissingDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath("sessions_dir", filepath.Join(t.TempDir(), "missing"))

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
	// No credentials under the isolated HOME: quota fetch stays inert.
	t.Setenv("HOME", t.TempDir())

	// Session 1 in group-a.
	s1 := filepath.Join(root, "group-a", "sess-1")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines1 := "" +
		`{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":1000,"output":500,"input_cache_read":200,"input_cache_creation":50}}}}` + "\n" +
		`{"timestamp":1735689700.5,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":300,"output":150}}}}` + "\n" +
		// Non-StatusUpdate frame — must be ignored.
		`{"timestamp":1735689800.0,"message":{"type":"UserMessage","payload":{"text":"x"}}}` + "\n"
	if err := os.WriteFile(filepath.Join(s1, "wire.jsonl"), []byte(lines1), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Session 2 in group-b.
	s2 := filepath.Join(root, "group-b", "sess-2")
	if err := os.MkdirAll(s2, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	lines2 := `{"timestamp":1735689900.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":2000,"output":1000}}}}` + "\n"
	if err := os.WriteFile(filepath.Join(s2, "wire.jsonl"), []byte(lines2), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Sibling non-wire file: must be ignored.
	if err := os.WriteFile(filepath.Join(s2, "metadata.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Custom config.json driving model name.
	cfgPath := filepath.Join(t.TempDir(), "kimi-config.json")
	if err := os.WriteFile(cfgPath, []byte(`{"model":"kimi-k2-preview"}`), 0o600); err != nil {
		t.Fatalf("write cfg: %v", err)
	}

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath("sessions_dir", root)
	acct.SetPath("config_path", cfgPath)

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

	// 2 unique sessions seen.
	expect("total_sessions", 2)
	// Input totals: 1000+300+2000 = 3300; output: 500+150+1000 = 1650.
	expect("total_input_tokens", 3300)
	expect("total_output_tokens", 1650)
	expect("total_cache_read", 200)
	expect("total_cache_write", 50)
	expect("total_tokens", 3300+1650)

	if len(snap.ModelUsage) != 1 {
		t.Fatalf("len(ModelUsage) = %d, want 1", len(snap.ModelUsage))
	}
	rec := snap.ModelUsage[0]
	if rec.RawModelID != "kimi-k2-preview" {
		t.Errorf("RawModelID = %q, want kimi-k2-preview (from config.json)", rec.RawModelID)
	}
	if rec.Dimensions["upstream_provider"] != "moonshot" {
		t.Errorf("upstream_provider = %q, want moonshot", rec.Dimensions["upstream_provider"])
	}
	if rec.Requests == nil || *rec.Requests != 3 {
		got := -1.0
		if rec.Requests != nil {
			got = *rec.Requests
		}
		t.Errorf("Requests = %v, want 3", got)
	}
	if rec.RawSource != "jsonl" {
		t.Errorf("RawSource = %q, want jsonl", rec.RawSource)
	}
}

func TestProvider_Fetch_EmptyDir(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", t.TempDir())
	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Errorf("status = %v, want OK", snap.Status)
	}
	if snap.Message == "" {
		t.Error("expected human-readable message for empty dir")
	}
}

func TestProvider_Fetch_DefaultModelFallback(t *testing.T) {
	// No config.json provided: per-record model lookup falls back to the
	// kimi-for-coding constant.
	root := t.TempDir()
	s1 := filepath.Join(root, "g", "s")
	if err := os.MkdirAll(s1, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	line := `{"timestamp":1735689600.0,"message":{"type":"StatusUpdate","payload":{"token_usage":{"input_other":10,"output":5}}}}`
	if err := os.WriteFile(filepath.Join(s1, "wire.jsonl"), []byte(line+"\n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Point HOME elsewhere so resolveConfigPath returns "".
	t.Setenv("HOME", t.TempDir())

	p := New()
	p.clock = fixedClock{t: time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)}
	acct := core.AccountConfig{ID: "kimi_cli", Provider: "kimi_cli", Auth: "local"}
	acct.SetPath("sessions_dir", root)

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("status = %v, want OK", snap.Status)
	}
	if len(snap.ModelUsage) != 1 {
		t.Fatalf("ModelUsage len = %d, want 1", len(snap.ModelUsage))
	}
	if snap.ModelUsage[0].RawModelID != defaultModel {
		t.Errorf("model = %q, want %q", snap.ModelUsage[0].RawModelID, defaultModel)
	}
}
