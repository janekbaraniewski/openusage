package codex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
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
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(sessionsDir, "rollout-2026-02-10T00-00-00-test.jsonl")
	sessionContent := `{"timestamp":"2026-02-10T00:00:01Z","type":"session_meta","payload":{"id":"test-session","cwd":"/tmp"}}
{"timestamp":"2026-02-10T00:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":1000,"cached_input_tokens":500,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"last_token_usage":{"input_tokens":500,"cached_input_tokens":250,"output_tokens":100,"reasoning_output_tokens":25,"total_tokens":600},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":10.5,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":75.0,"window_minutes":10080,"resets_at":1770934095},"credits":{"has_credits":false,"unlimited":false,"balance":null},"plan_type":null}}}
{"timestamp":"2026-02-10T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":2000,"cached_input_tokens":1000,"output_tokens":400,"reasoning_output_tokens":100,"total_tokens":2400},"last_token_usage":{"input_tokens":1000,"cached_input_tokens":500,"output_tokens":200,"reasoning_output_tokens":50,"total_tokens":1200},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":20.0,"window_minutes":300,"resets_at":1770700100},"secondary":{"used_percent":80.0,"window_minutes":10080,"resets_at":1770934095},"credits":{"has_credits":true,"unlimited":false,"balance":50.0},"plan_type":"team"}}}
`
	if err := os.WriteFile(sessionFile, []byte(sessionContent), 0644); err != nil {
		t.Fatal(err)
	}

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

	if snap.Status != core.StatusOK {
		t.Errorf("expected status OK, got %v", snap.Status)
	}

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

	if reset, ok := snap.Resets["rate_limit_primary"]; ok {
		if reset.Unix() != 1770700100 {
			t.Errorf("expected primary reset at 1770700100, got %d", reset.Unix())
		}
	} else {
		t.Error("expected rate_limit_primary reset time")
	}

	if snap.Raw["credits"] != "available" {
		t.Errorf("expected credits 'available', got %q", snap.Raw["credits"])
	}
	if snap.Raw["credit_balance"] != "$50.00" {
		t.Errorf("expected credit_balance '$50.00', got %q", snap.Raw["credit_balance"])
	}

	if snap.Raw["plan_type"] != "team" {
		t.Errorf("expected plan_type 'team', got %q", snap.Raw["plan_type"])
	}
	if snap.Raw["account_email"] != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got %q", snap.Raw["account_email"])
	}
	if snap.Raw["cli_version"] != "0.98.0" {
		t.Errorf("expected cli_version '0.98.0', got %q", snap.Raw["cli_version"])
	}

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

func TestFetchUsesLiveUsageEndpoint(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(sessionsDir, "rollout-test.jsonl")
	sessionContent := `{"timestamp":"2026-02-10T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":120},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":88.0,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":91.0,"window_minutes":10080,"resets_at":1770934095}}}}
`
	if err := os.WriteFile(sessionFile, []byte(sessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	authPath := filepath.Join(tmpDir, "auth.json")
	authContent := `{"tokens":{"access_token":"test-token","account_id":"acct-123"}}`
	if err := os.WriteFile(authPath, []byte(authContent), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			http.NotFound(w, r)
			return
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization header = %q, want Bearer test-token", got)
		}
		if got := r.Header.Get("ChatGPT-Account-Id"); got != "acct-123" {
			t.Fatalf("ChatGPT-Account-Id header = %q, want acct-123", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{
			"account_id":"acct-123",
			"email":"live@example.com",
			"plan_type":"team",
			"rate_limit":{
				"allowed":true,
				"limit_reached":false,
				"primary_window":{"used_percent":3,"limit_window_seconds":18000,"reset_at":1771688636},
				"secondary_window":{"used_percent":33,"limit_window_seconds":604800,"reset_at":1772218274}
			},
			"code_review_rate_limit":{
				"allowed":true,
				"limit_reached":false,
				"primary_window":{"used_percent":1,"limit_window_seconds":604800,"reset_at":1772275686},
				"secondary_window":null
			},
			"additional_rate_limits":[
				{
					"limit_name":"codex_other",
					"metered_feature":"codex_other",
					"rate_limit":{
						"allowed":true,
						"limit_reached":false,
						"primary_window":{"used_percent":55,"limit_window_seconds":3600,"reset_at":1771700000},
						"secondary_window":null
					}
				}
			],
			"credits":{"has_credits":true,"unlimited":false,"balance":"9.99"}
		}`)
	}))
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "codex-live",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":       tmpDir,
			"sessions_dir":     filepath.Join(tmpDir, "sessions"),
			"auth_file":        authPath,
			"chatgpt_base_url": server.URL + "/backend-api",
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := metricUsed(t, snap, "rate_limit_primary"); got != 3 {
		t.Fatalf("rate_limit_primary used = %.1f, want 3", got)
	}
	if got := metricUsed(t, snap, "rate_limit_secondary"); got != 33 {
		t.Fatalf("rate_limit_secondary used = %.1f, want 33", got)
	}
	if got := metricUsed(t, snap, "rate_limit_code_review_primary"); got != 1 {
		t.Fatalf("rate_limit_code_review_primary used = %.1f, want 1", got)
	}
	if got := metricUsed(t, snap, "rate_limit_codex_other_primary"); got != 55 {
		t.Fatalf("rate_limit_codex_other_primary used = %.1f, want 55", got)
	}
	if snap.Raw["account_email"] != "live@example.com" {
		t.Fatalf("account_email = %q, want live@example.com", snap.Raw["account_email"])
	}
	if snap.Raw["quota_api"] != "live" {
		t.Fatalf("quota_api = %q, want live", snap.Raw["quota_api"])
	}
	if snap.Raw["credit_balance"] != "$9.99" {
		t.Fatalf("credit_balance = %q, want $9.99", snap.Raw["credit_balance"])
	}
}

func TestFetchFallsBackToSessionWhenLiveUsageFails(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions", "2026", "02", "10")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionFile := filepath.Join(sessionsDir, "rollout-test.jsonl")
	sessionContent := `{"timestamp":"2026-02-10T00:00:01Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":0,"output_tokens":50,"reasoning_output_tokens":0,"total_tokens":150},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":20.0,"window_minutes":300,"resets_at":1770700000},"secondary":{"used_percent":80.0,"window_minutes":10080,"resets_at":1770934095}}}}
`
	if err := os.WriteFile(sessionFile, []byte(sessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	authPath := filepath.Join(tmpDir, "auth.json")
	authContent := `{"tokens":{"access_token":"test-token","account_id":"acct-123"}}`
	if err := os.WriteFile(authPath, []byte(authContent), 0644); err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backend-api/wham/usage" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, `{"error":"boom"}`)
	}))
	defer server.Close()

	p := New()
	acct := core.AccountConfig{
		ID:       "codex-fallback",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":       tmpDir,
			"sessions_dir":     filepath.Join(tmpDir, "sessions"),
			"auth_file":        authPath,
			"chatgpt_base_url": server.URL + "/backend-api",
		},
	}

	snap, err := p.Fetch(context.Background(), acct)
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := metricUsed(t, snap, "rate_limit_primary"); got != 20 {
		t.Fatalf("rate_limit_primary used = %.1f, want 20", got)
	}
	if !strings.Contains(snap.Raw["quota_api_error"], "HTTP 500") {
		t.Fatalf("quota_api_error = %q, want HTTP 500", snap.Raw["quota_api_error"])
	}
}

func TestFetchBuildsModelAndClientUsageSplits(t *testing.T) {
	tmpDir := t.TempDir()
	sessionsRoot := filepath.Join(tmpDir, "sessions")
	dayDir := filepath.Join(sessionsRoot, "2026", "02", "10")
	if err := os.MkdirAll(dayDir, 0755); err != nil {
		t.Fatal(err)
	}

	cliSession := filepath.Join(dayDir, "rollout-cli.jsonl")
	cliContent := `{"timestamp":"2026-02-10T00:00:01Z","type":"session_meta","payload":{"id":"cli-1","source":"cli","originator":"codex_cli_rs"}}
{"timestamp":"2026-02-10T00:00:02Z","type":"turn_context","payload":{"model":"gpt-5-codex"}}
{"timestamp":"2026-02-10T00:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":80,"cached_input_tokens":20,"output_tokens":20,"reasoning_output_tokens":0,"total_tokens":100},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":1.0,"window_minutes":300,"resets_at":1770700000}}}}
{"timestamp":"2026-02-10T00:00:04Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}
{"timestamp":"2026-02-10T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":120,"cached_input_tokens":30,"output_tokens":40,"reasoning_output_tokens":0,"total_tokens":160},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":2.0,"window_minutes":300,"resets_at":1770700001}}}}
`
	if err := os.WriteFile(cliSession, []byte(cliContent), 0644); err != nil {
		t.Fatal(err)
	}

	desktopSession := filepath.Join(dayDir, "rollout-desktop.jsonl")
	desktopContent := `{"timestamp":"2026-02-10T01:00:01Z","type":"session_meta","payload":{"id":"desktop-1","source":"vscode","originator":"Codex Desktop"}}
{"timestamp":"2026-02-10T01:00:02Z","type":"turn_context","payload":{"model":"gpt-5.3-codex"}}
{"timestamp":"2026-02-10T01:00:03Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":40,"cached_input_tokens":5,"output_tokens":10,"reasoning_output_tokens":0,"total_tokens":50},"model_context_window":128000},"rate_limits":{"primary":{"used_percent":3.0,"window_minutes":300,"resets_at":1770700002}}}}
`
	if err := os.WriteFile(desktopSession, []byte(desktopContent), 0644); err != nil {
		t.Fatal(err)
	}

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "codex-split",
		Provider: "codex",
		Auth:     "local",
		ExtraData: map[string]string{
			"config_dir":   tmpDir,
			"sessions_dir": sessionsRoot,
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}

	if got := metricUsed(t, snap, "model_gpt_5_codex_total_tokens"); got != 100 {
		t.Fatalf("model_gpt_5_codex_total_tokens = %.1f, want 100", got)
	}
	if got := metricUsed(t, snap, "model_gpt_5_3_codex_total_tokens"); got != 110 {
		t.Fatalf("model_gpt_5_3_codex_total_tokens = %.1f, want 110", got)
	}
	if got := metricUsed(t, snap, "client_cli_total_tokens"); got != 160 {
		t.Fatalf("client_cli_total_tokens = %.1f, want 160", got)
	}
	if got := metricUsed(t, snap, "client_desktop_app_total_tokens"); got != 50 {
		t.Fatalf("client_desktop_app_total_tokens = %.1f, want 50", got)
	}
	if !strings.Contains(snap.Raw["model_usage"], "gpt-5.3-codex") || !strings.Contains(snap.Raw["model_usage"], "gpt-5-codex") {
		t.Fatalf("model_usage = %q, expected both models", snap.Raw["model_usage"])
	}
	if !strings.Contains(snap.Raw["client_usage"], "CLI") || !strings.Contains(snap.Raw["client_usage"], "Desktop App") {
		t.Fatalf("client_usage = %q, expected CLI and Desktop App", snap.Raw["client_usage"])
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

	olderDir := filepath.Join(tmpDir, "2026", "01", "01")
	os.MkdirAll(olderDir, 0755)
	olderFile := filepath.Join(olderDir, "rollout-older.jsonl")
	os.WriteFile(olderFile, []byte("{}"), 0644)

	time.Sleep(10 * time.Millisecond)

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

func metricUsed(t *testing.T, snap core.QuotaSnapshot, key string) float64 {
	t.Helper()
	metric, ok := snap.Metrics[key]
	if !ok {
		t.Fatalf("missing metric %q", key)
	}
	if metric.Used == nil {
		t.Fatalf("metric %q has nil Used", key)
	}
	return *metric.Used
}
