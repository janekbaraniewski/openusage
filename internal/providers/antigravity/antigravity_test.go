package antigravity

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func TestCaptureStatusLineAndFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "antigravity-status.json")
	line, err := CaptureStatusLine([]byte(sampleStatusLineJSON), path)
	if err != nil {
		t.Fatalf("CaptureStatusLine() error = %v", err)
	}
	if want := "AGY · Gemini Pro · quota 12% · context 15%"; line != want {
		t.Fatalf("CaptureStatusLine() = %q, want %q", line, want)
	}

	// Windows does not preserve Unix permission bits in os.FileMode. The
	// production path still requests 0600 on platforms where it is meaningful.
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat state file: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Fatalf("state file mode = %o, want 600", got)
		}
	}

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"status_file": path,
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusNearLimit {
		t.Fatalf("Fetch() status = %q, want %q", snap.Status, core.StatusNearLimit)
	}
	if got := metricUsed(t, snap, "quota"); got != 88 {
		t.Fatalf("quota used = %v, want 88", got)
	}
	if got := metricRemaining(t, snap, "context_window"); got != 85 {
		t.Fatalf("context remaining = %v, want 85", got)
	}
	if got := metricUsed(t, snap, "total_tokens"); got != 1500 {
		t.Fatalf("total tokens = %v, want 1500", got)
	}
	if got := metricUsed(t, snap, "current_tokens"); got != 147 {
		t.Fatalf("current tokens = %v, want 147", got)
	}
	if got := snap.Attributes["workspace"]; got != "/tmp/antigravity-project" {
		t.Fatalf("workspace = %q, want project path", got)
	}
	if len(snap.ModelUsage) != 1 || snap.ModelUsage[0].RawModelID != "Gemini Pro" {
		t.Fatalf("model usage = %+v, want one Gemini Pro row", snap.ModelUsage)
	}
	if snap.Resets["quota_pro_reset"].IsZero() || snap.Resets["quota_reset"].IsZero() {
		t.Fatal("expected quota reset timestamps")
	}
}

func TestFetchMissingStatusLineIsNonFatal(t *testing.T) {
	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"status_file": filepath.Join(t.TempDir(), "missing.json"),
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusAuth {
		t.Fatalf("status = %q, want %q", snap.Status, core.StatusAuth)
	}
	if !strings.Contains(snap.Message, "No Antigravity") {
		t.Fatalf("message = %q, want setup guidance", snap.Message)
	}
}

func TestFetchCanceledContextKeepsStatusFileDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "antigravity-status.json")

	snap, err := New().Fetch(ctx, core.AccountConfig{
		ID:       "antigravity",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"status_file": path,
		},
	})
	if err == nil {
		t.Fatal("Fetch() error = nil, want canceled context error")
	}
	if got := snap.Raw["status_file"]; got != path {
		t.Fatalf("status_file diagnostic = %q, want %q", got, path)
	}
}

func TestTelemetryRevisionAndCurrentUsage(t *testing.T) {
	p := New()
	events, err := p.ParseHookPayload([]byte(sampleStatusLineJSON), shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("ParseHookPayload() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("ParseHookPayload() returned %d events, want 1", len(events))
	}
	event := events[0]
	if event.EventType != shared.TelemetryEventTypeMessageUsage {
		t.Fatalf("event type = %q, want message usage", event.EventType)
	}
	if event.InputTokens == nil || *event.InputTokens != 100 {
		t.Fatalf("input tokens = %v, want 100", event.InputTokens)
	}
	if event.TotalTokens == nil || *event.TotalTokens != 147 {
		t.Fatalf("total tokens = %v, want 147", event.TotalTokens)
	}
	if event.TurnID == "" || !strings.Contains(event.TurnID, ":status:") {
		t.Fatalf("turn ID = %q, want stable status revision", event.TurnID)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(sampleStatusLineJSON), &payload); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	contextWindow := payload["context_window"].(map[string]any)
	contextWindow["total_output_tokens"] = float64(301)
	changed, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal changed payload: %v", err)
	}
	changedEvents, err := p.ParseHookPayload(changed, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("ParseHookPayload(changed) error = %v", err)
	}
	if changedEvents[0].TurnID == event.TurnID {
		t.Fatal("changed cumulative usage reused the same revision")
	}
}

func TestParseStatusLineRejectsMalformedJSON(t *testing.T) {
	if _, err := New().ParseHookPayload([]byte("not-json"), shared.TelemetryCollectOptions{}); err == nil {
		t.Fatal("ParseHookPayload() accepted malformed JSON")
	}
	if got := RenderStatusLine([]byte("not-json")); got != "AGY" {
		t.Fatalf("RenderStatusLine(malformed) = %q, want AGY", got)
	}
}

func metricUsed(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	metric, ok := snap.Metrics[key]
	if !ok || metric.Used == nil {
		t.Fatalf("metric %q missing used value: %+v", key, metric)
	}
	return *metric.Used
}

func metricRemaining(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	metric, ok := snap.Metrics[key]
	if !ok || metric.Remaining == nil {
		t.Fatalf("metric %q missing remaining value: %+v", key, metric)
	}
	return *metric.Remaining
}

func TestProjectQuotaMetrics_AdvancesPastReset(t *testing.T) {
	past := time.Now().UTC().Add(-10 * time.Minute)
	payload := statusLinePayload{
		ReceivedAt: past.Add(-5 * time.Minute),
		Quota: map[string]statusLineQuota{
			"gemini-weekly": {
				RemainingFraction: core.Float64Ptr(0),
				ResetTime:         past.Format(time.RFC3339Nano),
			},
		},
	}
	var snap core.UsageSnapshot
	snap.Metrics = make(map[string]core.Metric)
	snap.Resets = make(map[string]time.Time)

	projectQuotaMetrics(&snap, payload)

	if rem := metricRemaining(t, snap, "quota_gemini_weekly"); rem != 100 {
		t.Fatalf("quota_gemini_weekly remaining = %v, want 100 after reset passed", rem)
	}
	reset, ok := snap.Resets["quota_gemini_weekly"]
	if !ok {
		t.Fatal("missing quota_gemini_weekly reset")
	}
	if !reset.After(time.Now().UTC()) {
		t.Fatalf("reset time = %v, expected future timestamp after advancing weekly period", reset)
	}
}

const sampleStatusLineJSON = `{
  "cwd": "/tmp/antigravity-project",
  "session_id": "session-1",
  "conversation_id": "conversation-1",
  "model": {"id": "gemini-pro", "display_name": "Gemini Pro"},
  "workspace": {"current_dir": "/tmp/antigravity-project", "project_dir": "/tmp/antigravity-project"},
  "version": "1.1.13",
  "product": "antigravity",
  "context_window": {
    "total_input_tokens": 1200,
    "total_output_tokens": 300,
    "context_window_size": 10000,
    "used_percentage": 15,
    "remaining_percentage": 85,
    "current_usage": {
      "input_tokens": 100,
      "output_tokens": 40,
      "cache_read_input_tokens": 5,
      "cache_creation_input_tokens": 2
    }
  },
  "quota": {
    "pro": {"remaining_fraction": 0.12, "reset_time": "2030-01-02T03:04:05Z"},
    "flash": {"remaining_fraction": 0.50, "reset_in_seconds": 3600}
  },
  "agent_state": "working",
  "plan_tier": "pro",
  "email": "amanda@example.com"
}
`

func TestFetchGemini5hExhaustedWeeklyAvailable(t *testing.T) {
	const jsonPayload = `{
		"agent_state": "idle",
		"email": "nurulislamz2600@gmail.com",
		"model": {"id": "Gemini 3.7 Flash (High)", "display_name": "Gemini 3.7 Flash (High)", "effort": "high"},
		"plan_tier": "Google AI Pro",
		"product": "antigravity",
		"quota": {
			"3p-5h": {"remaining_fraction": 0.6616888, "reset_time": "2030-08-29T19:39:50Z", "disabled": true},
			"3p-weekly": {"remaining_fraction": 0, "reset_time": "2030-09-02T23:42:57Z"},
			"gemini-5h": {"remaining_fraction": 0, "reset_time": "2030-08-29T16:49:43Z"},
			"gemini-weekly": {"remaining_fraction": 0.3787134, "reset_time": "2030-09-02T03:31:19Z"}
		}
	}`

	path := filepath.Join(t.TempDir(), "antigravity-nurulz-status.json")
	if err := os.WriteFile(path, []byte(jsonPayload), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity-nurulz",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"status_file": path,
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if snap.Status != core.StatusLimited {
		t.Fatalf("snap.Status = %q, want %q", snap.Status, core.StatusLimited)
	}
	if got := *snap.Metrics["quota_gemini_weekly"].Remaining; math.Abs(got-37.87134) > 0.001 {
		t.Errorf("quota_gemini_weekly = %v, want 37.87134", got)
	}
	if got := *snap.Metrics["quota_gemini_5h"].Remaining; got != 0 {
		t.Errorf("quota_gemini_5h = %v, want 0", got)
	}
	// Reset time for quota should be the active model's limiting reset (gemini-5h reset at 16:49:43Z), NOT 3p-weekly
	wantReset, _ := time.Parse(time.RFC3339, "2030-08-29T16:49:43Z")
	if !snap.Resets["quota"].Equal(wantReset) {
		t.Errorf("snap.Resets[\"quota\"] = %v, want %v", snap.Resets["quota"], wantReset)
	}
}

func TestFetchChaosUsageShowsAllModels(t *testing.T) {
	const jsonPayload = `{
		"agent_state": "working",
		"artifact_count": 1,
		"context_window": {
			"total_input_tokens": 122927,
			"total_output_tokens": 129250,
			"context_window_size": 1048576,
			"used_percentage": 11.72323226928711,
			"remaining_percentage": 88.27676773071289
		},
		"email": "chaosfury935@gmail.com",
		"model": {"id": "Gemini 3.7 Flash (High)", "display_name": "Gemini 3.7 Flash (High)", "effort": "high"},
		"plan_tier": "Google AI Pro",
		"product": "antigravity",
		"quota": {
			"3p-5h": {"remaining_fraction": 1, "reset_time": "2026-08-29T21:33:37Z", "reset_in_seconds": 17722},
			"3p-weekly": {"remaining_fraction": 1, "reset_time": "2026-09-05T16:33:37Z", "reset_in_seconds": 604522},
			"gemini-5h": {"remaining_fraction": 0.7652325, "reset_time": "2026-08-29T20:25:53Z", "reset_in_seconds": 13658},
			"gemini-weekly": {"remaining_fraction": 0.96087205, "reset_time": "2026-09-05T15:25:53Z", "reset_in_seconds": 600458}
		}
	}`

	path := filepath.Join(t.TempDir(), "antigravity-chaos-status.json")
	if err := os.WriteFile(path, []byte(jsonPayload), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "antigravity-chaos",
		Provider: "antigravity",
		ProviderPaths: map[string]string{
			"status_file": path,
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}

	if snap.Status != core.StatusOK {
		t.Fatalf("snap.Status = %q, want %q", snap.Status, core.StatusOK)
	}
	if snap.Attributes["claude_disabled"] == "true" {
		t.Errorf("claude_disabled should not be set for chaos account")
	}

	// Verify both Gemini and Claude/GPT 3P quotas are present and populated
	if m, ok := snap.Metrics["quota_3p_weekly"]; !ok || m.Remaining == nil || *m.Remaining != 100 {
		t.Errorf("quota_3p_weekly = %v, want 100", m)
	}
	if m, ok := snap.Metrics["quota_3p_5h"]; !ok || m.Remaining == nil || *m.Remaining != 100 {
		t.Errorf("quota_3p_5h = %v, want 100", m)
	}
	if m, ok := snap.Metrics["quota_claude_weekly"]; !ok || m.Remaining == nil || *m.Remaining != 100 {
		t.Errorf("quota_claude_weekly = %v, want 100", m)
	}
	if m, ok := snap.Metrics["quota_claude_5h"]; !ok || m.Remaining == nil || *m.Remaining != 100 {
		t.Errorf("quota_claude_5h = %v, want 100", m)
	}
	if m, ok := snap.Metrics["quota_gemini_weekly"]; !ok || m.Remaining == nil || math.Abs(*m.Remaining-96.0872) > 0.01 {
		t.Errorf("quota_gemini_weekly = %v, want ~96.09", m)
	}
	if m, ok := snap.Metrics["quota_gemini_5h"]; !ok || m.Remaining == nil || math.Abs(*m.Remaining-76.5232) > 0.01 {
		t.Errorf("quota_gemini_5h = %v, want ~76.52", m)
	}
}
