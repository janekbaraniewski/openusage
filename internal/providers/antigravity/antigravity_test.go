package antigravity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

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
}`
