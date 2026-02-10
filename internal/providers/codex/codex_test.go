package codex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/agentusage/internal/core"
)

func TestProviderID(t *testing.T) {
	p := New()
	if p.ID() != "codex" {
		t.Errorf("expected ID 'codex', got %q", p.ID())
	}
}

func TestDescribe(t *testing.T) {
	p := New()
	info := p.Describe()
	if info.Name != "OpenAI Codex CLI" {
		t.Errorf("expected name 'OpenAI Codex CLI', got %q", info.Name)
	}
	if len(info.Capabilities) == 0 {
		t.Error("expected at least one capability")
	}
}

func TestFetchWithSessionData(t *testing.T) {
	// Create a temporary directory structure mimicking ~/.codex
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a session JSONL file with token_count events
	sessionFile := filepath.Join(sessionsDir, "rollout-2026-02-10T00-00-00-test.jsonl")
	sessionContent := `{"timestamp":"2026-02-10T00:00:01Z","type":"session_meta","payload":{"id":"test-session","cwd":"/tmp"}}
{"timestamp":"2026-02-10T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":500,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"last_token_usage":{"input_tokens":500,"cached_input_tokens":250,"output_tokens":100,"reasoning_output_tokens":25,"total_tokens":600},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":10.5,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":75.0,"window_minutes":10080,"resets_at":1770934095},"credits":{"has_credits":false,"unlimited":false,"balance":null},"plan_type":null}}}
{"timestamp":"2026-02-10T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2000,"cached_input_tokens":1000,"output_tokens":400,"reasoning_output_tokens":100,"total_tokens":2400},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":500,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":20.0,"window_minutes":300,"resets_at":1770700100},"secondary":{"used_percent":80.0,"window_minutes":10080,"resets_at":1770934095},"credits":{"has_credits":true,"unlimited":false,"balance":50.0},"plan_type":"team"}}}
`
	if err := os.WriteFile(sessionFile, []byte(sessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create version.json
	versionFile := filepath.Join(tmpDir, "version.json")
	if err := os.WriteFile(versionFile, []byte(`{"latest_version":"0.98.0"}`), 0644); err != nil {
		t.Fatal(err)
	}

	p := New()
	acct := core.AccountConfig{
		ID:       "codex-test",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":   tmpDir,
			"sessions_dir": filepath.Join(tmpDir, "sessions"),
			"email":        "test@example.com",
			"plan_type":    "team",
			"account_id":   "test-account-123",
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}

	// Verify status
	if snap.Status != core.StatusOK {
		t.Errorf("expected status OK, got %v", snap.Status)
	}

	// Verify it got the LAST token_count event (2000 input tokens, not 1000)
	if m, ok := snap.Metrics["session_input_tokens"]; ok {
		if m.Used == nil || *m.Used != 2000 {
			t.Errorf("expected session_input_tokens=2000, got %v", m.Used)
		}
	} else {
		t.Error("expected session_input_tokens metric")
	}

	if m, ok := snap.Metrics["session_output_tokens"]; ok {
		if m.Used == nil || *m.Used != 400 {
			t.Errorf("expected session_output_tokens=400, got %v", m.Used)
		}
	} else {
		t.Error("expected session_output_tokens metric")
	}

	if m, ok := snap.Metrics["session_reasoning_tokens"]; ok {
		if m.Used == nil || *m.Used != 100 {
			t.Errorf("expected session_reasoning_tokens=100, got %v", m.Used)
		}
	} else {
		t.Error("expected session_reasoning_tokens metric")
	}

	// Verify rate limits
	if m, ok := snap.Metrics["rate_limit_primary"]; ok {
		if m.Used == nil || *m.Used != 20.0 {
			t.Errorf("expected primary used=20.0, got %v", m.Used)
		}
		if m.Remaining == nil || *m.Remaining != 80.0 {
			t.Errorf("expected primary remaining=80.0, got %v", m.Remaining)
		}
		if m.Window != "5h" {
			t.Errorf("expected window '5h', got %q", m.Window)
		}
	} else {
		t.Error("expected rate_limit_primary metric")
	}

	if m, ok := snap.Metrics["rate_limit_secondary"]; ok {
		if m.Used == nil || *m.Used != 80.0 {
			t.Errorf("expected secondary used=80.0, got %v", m.Used)
		}
		if m.Window != "7d" {
			t.Errorf("expected window '7d', got %q", m.Window)
		}
	} else {
		t.Error("expected rate_limit_secondary metric")
	}

	// Verify resets are set
	if reset, ok := snap.Resets["rate_limit_primary"]; ok {
		if reset.Unix() != 1770700100 {
			t.Errorf("expected primary reset at 1770700100, got %d", reset.Unix())
		}
	} else {
		t.Error("expected rate_limit_primary reset time")
	}

	// Verify credits
	if snap.Raw["credits"] != "available" {
		t.Errorf("expected credits 'available', got %q", snap.Raw["credits"])
	}
	if snap.Raw["credit_balance"] != "$50.00" {
		t.Errorf("expected credit_balance '$50.00', got %q", snap.Raw["credit_balance"])
	}

	// Verify metadata
	if snap.Raw["plan_type"] != "team" {
		t.Errorf("expected plan_type 'team', got %q", snap.Raw["plan_type"])
	}
	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %q", snap.Raw["account_email"])
	}
	if snap.Raw["cli_version"] != "0.98.0" {
		t.Errorf("expected cli_version '0.98.0', got %q", snap.Raw["cli_version"])
	}

	// Verify context window metric
	if m, ok := snap.Metrics["context_window"]; ok {
		if m.Limit == nil || *m.Limit != 128000 {
			t.Errorf("expected context_window limit=128000, got %v", m.Limit)
		}
		if m.Used == nil || *m.Used != 2000 {
			t.Errorf("expected context_window used=2000, got %v", m.Used)
		}
	} else {
		t.Error("expected context_window metric")
	}
}

func TestFetchNearLimit(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	os.MkdirAll(sessionsDir, 0755)

	sessionFile := filepath.Join(sessionsDir, "rollout-test.jsonl")
	content := `{"timestamp":"2026-02-10T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":150},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":95.0,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":50.0,"window_minutes":10080,"resets_at":1770934095}}}}
`
	os.WriteFile(sessionFile, []byte(content), 0644)

	p := New()
	snap, _ := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "test",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":   tmpDir,
			"sessions_dir": filepath.Join(tmpDir, "sessions"),
		},
	})

	if snap.Status != core.StatusNearLimit {
		t.Errorf("expected NEAR_LIMIT status when primary is 95%%, got %v", snap.Status)
	}
}

func TestFetchLimited(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	os.MkdirAll(sessionsDir, 0755)

	sessionFile := filepath.Join(sessionsDir, "rollout-test.jsonl")
	content := `{"timestamp":"2026-02-10T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":150},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":100.0,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":50.0,"window_minutes":10080,"resets_at":1770934095}}}}
`
	os.WriteFile(sessionFile, []byte(content), 0644)

	p := New()
	snap, _ := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "test",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":   tmpDir,
			"sessions_dir": filepath.Join(tmpDir, "sessions"),
		},
	})

	if snap.Status != core.StatusLimited {
		t.Errorf("expected LIMITED status when primary is 100%%, got %v", snap.Status)
	}
}

func TestFetchNoSessions(t *testing.T) {
	tmpDir := t.TempDir()

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "test",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir": tmpDir,
		},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap.Status != core.StatusUnknown {
		t.Errorf("expected UNKNOWN status with no sessions, got %v", snap.Status)
	}
}

func TestFormatWindow(t *testing.T) {
	tests := []struct {
		minutes  int
		expected string
	}{
		{0, ""},
		{30, "30m"},
		{60, "1h"},
		{300, "5h"},
		{1440, "1d"},
		{10080, "7d"},
		{1500, "1d1h"},
		{90, "1h30m"},
	}

	for _, tc := range tests {
		t.Run(fmt.Sprintf("%d_minutes", tc.minutes), func(t *testing.T) {
			got := formatWindow(tc.minutes)
			if got != tc.expected {
				t.Errorf("formatWindow(%d) = %q, want %q", tc.minutes, got, tc.expected)
			}
		})
	}
}

func TestFindLatestSessionFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create older session file
	olderDir := filepath.Join(tmpDir, "2026", "01", "01")
	os.MkdirAll(olderDir, 0755)
	olderFile := filepath.Join(olderDir, "rollout-older.jsonl")
	os.WriteFile(olderFile, []byte("{}"), 0644)

	// Sleep briefly to ensure different mtimes
	time.Sleep(10 * time.Millisecond)

	// Create newer session file
	newerDir := filepath.Join(tmpDir, "2026", "02", "10")
	os.MkdirAll(newerDir, 0755)
	newerFile := filepath.Join(newerDir, "rollout-newer.jsonl")
	os.WriteFile(newerFile, []byte("{}"), 0644)

	found, err := findLatestSessionFile(tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if found != newerFile {
		t.Errorf("expected %q, got %q", newerFile, found)
	}
}
