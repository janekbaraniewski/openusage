package cursor

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

const sampleCursorStatusLineJSON = `{
  "session_id": "9e1e136b0c36226ebf417f5af0faeb11",
  "session_name": "feature-task",
  "transcript_path": "/home/user/.agent-containers/physics/.cursor/chats/9e1e136b0c36226ebf417f5af0faeb11/transcript.jsonl",
  "render_width_chars": 120,
  "cwd": "/tmp/cursor-project",
  "autorun": false,
  "model": {
    "id": "claude-4-opus",
    "display_name": "Claude 4 Opus",
    "param_summary": "(Thinking)",
    "max_mode": true
  },
  "workspace": {
    "current_dir": "/tmp/cursor-project",
    "project_dir": "/tmp/cursor-project/.cursor/transcripts",
    "added_dirs": []
  },
  "version": "1.2.3",
  "output_style": {
    "name": "default"
  },
  "context_window": {
    "total_input_tokens": 1200,
    "total_output_tokens": 300,
    "context_window_size": 200000,
    "used_percentage": 15.0,
    "remaining_percentage": 85.0,
    "current_usage": {
      "input_tokens": 100,
      "output_tokens": 47,
      "cache_read_input_tokens": 0,
      "cache_creation_input_tokens": 0,
      "cache_write_tokens": 0
    }
  },
  "quota": {
    "pro": {
      "remaining_fraction": 0.12,
      "reset_time": "2026-08-29T23:59:59Z"
    }
  },
  "vim": {
    "mode": "NORMAL"
  },
  "worktree": {
    "name": "my-branch",
    "path": "/tmp/cursor-project"
  },
  "auth_info": {
    "email": "physicsxd2izi@gmail.com",
    "displayName": "Nurul Islam"
  }
}`

func TestCaptureStatusLineAndFetch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor-status.json")
	line, err := CaptureStatusLine([]byte(sampleCursorStatusLineJSON), path)
	if err != nil {
		t.Fatalf("CaptureStatusLine() error = %v", err)
	}
	if want := "Cursor · Claude 4 Opus (Thinking) · quota 12% · context 15%"; line != want {
		t.Fatalf("CaptureStatusLine() = %q, want %q", line, want)
	}

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
		ID:       "cursor",
		Provider: "cursor",
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
	if got := snap.Attributes["workspace"]; got != "/tmp/cursor-project" {
		t.Fatalf("workspace = %q, want project path", got)
	}
	if got := snap.Attributes["model"]; got != "Claude 4 Opus" {
		t.Fatalf("model = %q, want Claude 4 Opus", got)
	}
	if got := snap.Attributes["model_param"]; got != "(Thinking)" {
		t.Fatalf("model_param = %q, want (Thinking)", got)
	}
	if got := snap.Attributes["worktree"]; got != "my-branch" {
		t.Fatalf("worktree = %q, want my-branch", got)
	}
	if len(snap.ModelUsage) != 1 || snap.ModelUsage[0].RawModelID != "Claude 4 Opus" {
		t.Fatalf("model usage = %+v, want one Claude 4 Opus row", snap.ModelUsage)
	}
	if snap.Resets["quota_pro_reset"].IsZero() || snap.Resets["quota_reset"].IsZero() {
		t.Fatal("expected quota reset timestamps")
	}
}

func TestFetchMissingStatusLineIsNonFatal(t *testing.T) {
	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor",
		Provider: "cursor",
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
	if !strings.Contains(snap.Message, "No Cursor") {
		t.Fatalf("message = %q, want setup guidance", snap.Message)
	}
}

func TestFetchCanceledContextKeepsStatusFileDiagnostic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	path := filepath.Join(t.TempDir(), "cursor-status.json")

	snap, err := New().Fetch(ctx, core.AccountConfig{
		ID:       "cursor",
		Provider: "cursor",
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
	events, err := p.ParseHookPayload([]byte(sampleCursorStatusLineJSON), shared.TelemetryCollectOptions{})
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
	if err := json.Unmarshal([]byte(sampleCursorStatusLineJSON), &payload); err != nil {
		t.Fatalf("unmarshal sample: %v", err)
	}
	contextWindow := payload["context_window"].(map[string]any)
	contextWindow["total_output_tokens"] = float64(450)
	changed, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal changed payload: %v", err)
	}
	changedEvents, err := p.ParseHookPayload(changed, shared.TelemetryCollectOptions{})
	if err != nil {
		t.Fatalf("ParseHookPayload(changed) error = %v", err)
	}
	if changedEvents[0].TurnID == event.TurnID {
		t.Fatalf("turn ID did not advance when token counts changed: %q", event.TurnID)
	}
}

func TestHasChanged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cursor-status.json")
	if err := os.WriteFile(path, []byte(sampleCursorStatusLineJSON), 0o600); err != nil {
		t.Fatalf("write state file: %v", err)
	}
	p := New()
	acct := core.AccountConfig{
		ID:            "cursor",
		Provider:      "cursor",
		ProviderPaths: map[string]string{"status_file": path},
	}
	changed, err := p.HasChanged(acct, time.Now().Add(-1*time.Minute))
	if err != nil {
		t.Fatalf("HasChanged() error = %v", err)
	}
	if !changed {
		t.Fatal("HasChanged() = false, want true for modified file")
	}

	changed, err = p.HasChanged(acct, time.Now().Add(1*time.Minute))
	if err != nil {
		t.Fatalf("HasChanged() error = %v", err)
	}
	if changed {
		t.Fatal("HasChanged() = true, want false for future timestamp")
	}
}

func TestProvider_ID_and_Describe(t *testing.T) {
	p := New()
	if got := p.ID(); got != "cursor" {
		t.Errorf("ID() = %q, want 'cursor'", got)
	}
	spec := p.Spec()
	if spec.ID != "cursor" {
		t.Errorf("Spec.ID = %q, want 'cursor'", spec.ID)
	}
	if spec.Info.Name != "Cursor IDE" {
		t.Errorf("Spec.Info.Name = %q, want 'Cursor IDE'", spec.Info.Name)
	}
}

func metricUsed(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok || m.Used == nil {
		t.Fatalf("metric %q missing Used value: %+v", key, snap.Metrics)
	}
	return *m.Used
}

func metricRemaining(t *testing.T, snap core.UsageSnapshot, key string) float64 {
	t.Helper()
	m, ok := snap.Metrics[key]
	if !ok || m.Remaining == nil {
		t.Fatalf("metric %q missing Remaining value: %+v", key, snap.Metrics)
	}
	return *m.Remaining
}

func TestPhysicsAndNurulzMultiAccountIsolation(t *testing.T) {
	tempDir := t.TempDir()

	// 1. Setup physics container (unused, 100% capacity remaining)
	physicsConfigDir := filepath.Join(tempDir, ".agent-containers", "physics", ".cursor")
	if err := os.MkdirAll(physicsConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	physicsCLIConfig := `{
		"authInfo": {
			"email": "physicsxd2izi@gmail.com",
			"displayName": "Physics Profile"
		},
		"model": {
			"displayName": "Claude 3.7 Sonnet"
		}
	}`
	if err := os.WriteFile(filepath.Join(physicsConfigDir, "cli-config.json"), []byte(physicsCLIConfig), 0o600); err != nil {
		t.Fatal(err)
	}

	// 2. Setup nurulz container (active usage)
	nurulzConfigDir := filepath.Join(tempDir, ".agent-containers", "nurulz", ".cursor")
	if err := os.MkdirAll(nurulzConfigDir, 0o755); err != nil {
		t.Fatal(err)
	}
	nurulzCLIConfig := `{
		"authInfo": {
			"email": "nurulislamz2600@gmail.com",
			"displayName": "Nurul Profile"
		}
	}`
	if err := os.WriteFile(filepath.Join(nurulzConfigDir, "cli-config.json"), []byte(nurulzCLIConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	nurulzStatusFile := filepath.Join(tempDir, "cursor-nurulz-status.json")
	nurulzStatusJSON := `{
		"session_id": "nurulz-active-session",
		"email": "nurulislamz2600@gmail.com",
		"context_window": {
			"total_input_tokens": 50000,
			"total_output_tokens": 12000,
			"used_percentage": 45.0,
			"remaining_percentage": 55.0
		},
		"quota": {
			"pro": {
				"remaining_fraction": 0.55
			}
		}
	}`
	if err := os.WriteFile(nurulzStatusFile, []byte(nurulzStatusJSON), 0o600); err != nil {
		t.Fatal(err)
	}

	p := New()

	acctPhysics := core.AccountConfig{
		ID:       "cursor-physics",
		Provider: "cursor",
		Auth:     "local",
		ProviderPaths: map[string]string{
			"config_dir":  physicsConfigDir,
			"status_file": filepath.Join(tempDir, "cursor-physics-status.json"), // does not exist yet
		},
	}
	acctNurulz := core.AccountConfig{
		ID:       "cursor-nurulz",
		Provider: "cursor",
		Auth:     "local",
		ProviderPaths: map[string]string{
			"config_dir":  nurulzConfigDir,
			"status_file": nurulzStatusFile,
		},
	}

	ctx := context.Background()
	snapPhysics, err := p.Fetch(ctx, acctPhysics)
	if err != nil {
		t.Fatalf("Fetch(physics) error = %v", err)
	}
	snapNurulz, err := p.Fetch(ctx, acctNurulz)
	if err != nil {
		t.Fatalf("Fetch(nurulz) error = %v", err)
	}

	if snapPhysics.Status != core.StatusOK {
		t.Fatalf("expected physics status OK, got %v (%s)", snapPhysics.Status, snapPhysics.Message)
	}
	if snapPhysics.Attributes["email"] != "physicsxd2izi@gmail.com" {
		t.Fatalf("physics email = %q", snapPhysics.Attributes["email"])
	}
	if _, ok := snapPhysics.Metrics["cursor_plan_usage"]; ok {
		t.Fatal("cli-config-only physics account must not invent 100% plan usage")
	}

	if snapNurulz.Status != core.StatusOK {
		t.Fatalf("expected nurulz status OK, got %v (%s)", snapNurulz.Status, snapNurulz.Message)
	}
	if got := metricRemaining(t, snapNurulz, "context_window"); got != 55.0 {
		t.Errorf("expected nurulz context remaining 55%%, got %.2f%%", got)
	}
	if snapNurulz.Attributes["account_email"] == snapPhysics.Attributes["email"] {
		t.Fatal("multi-account isolation failure: physics and nurulz share the same email")
	}
}

func TestFetchLivePlanFillsAutoAndAPI(t *testing.T) {
	ts := startNestedPlanUsageServer(t)
	defer ts.Close()
	path := filepath.Join(t.TempDir(), "cursor-status.json")
	if err := os.WriteFile(path, []byte(`{
		"model": {"display_name": "Auto"},
		"context_window": {"used_percentage": 10.0, "remaining_percentage": 90.0}
	}`), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor",
		Provider: "cursor",
		Token:    "test-jwt",
		BaseURL:  ts.URL,
		ProviderPaths: map[string]string{
			"status_file": path,
			"state_db":    filepath.Join(t.TempDir(), "missing.vscdb"),
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if got := metricUsed(t, snap, "plan_percent_used"); got != 7 {
		t.Fatalf("Included = %v, want 7", got)
	}
	if got := metricUsed(t, snap, "plan_auto_percent_used"); got != 6 {
		t.Fatalf("Auto = %v, want 6", got)
	}
	if got := metricUsed(t, snap, "plan_api_percent_used"); got != 29 {
		t.Fatalf("API = %v, want 29", got)
	}
	if snap.Attributes["plan_tier"] != "Pro" {
		t.Fatalf("plan_tier = %q, want Pro", snap.Attributes["plan_tier"])
	}
	if snap.Attributes["ondemand"] != "disabled" {
		t.Fatalf("ondemand = %q, want disabled", snap.Attributes["ondemand"])
	}
}

func TestFetchLivePlan_UsesBoxAuthJSONNotHostStateDB(t *testing.T) {
	var gotAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"billingCycleEnd": "1790685824000",
			"planUsage": {
				"autoPercentUsed": 0.56,
				"apiPercentUsed": 0,
				"totalPercentUsed": 0.5090909090909091
			}
		}`))
	}))
	defer ts.Close()

	home := t.TempDir()
	boxRoot := filepath.Join(home, ".agent-containers", "physics")
	configDir := filepath.Join(boxRoot, ".cursor")
	authDir := filepath.Join(boxRoot, ".config", "cursor")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(authDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "cli-config.json"), []byte(`{"authInfo":{"email":"physicsxd2izi@gmail.com"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(authDir, "auth.json"), []byte(`{"accessToken":"physics-box-jwt","refreshToken":"refresh"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	snap, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:       "cursor-physics",
		Provider: "cursor",
		BaseURL:  ts.URL,
		ProviderPaths: map[string]string{
			"config_dir":  configDir,
			"status_file": filepath.Join(home, "cursor-physics-status.json"),
			"state_db":    filepath.Join(home, "missing.vscdb"),
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if gotAuth != "Bearer physics-box-jwt" {
		t.Fatalf("Authorization = %q, want Bearer physics-box-jwt from auth.json", gotAuth)
	}
	if got := metricUsed(t, snap, "plan_percent_used"); math.Abs(got-0.5090909090909091) > 0.0001 {
		t.Fatalf("included = %v, want 0.509...", got)
	}
	if got := metricUsed(t, snap, "plan_auto_percent_used"); math.Abs(got-0.56) > 0.0001 {
		t.Fatalf("auto = %v, want 0.56", got)
	}
	if got := metricUsed(t, snap, "plan_api_percent_used"); got != 0 {
		t.Fatalf("api = %v, want 0", got)
	}
	reset, ok := snap.Resets["plan_percent_used"]
	if !ok || reset.IsZero() {
		t.Fatal("expected billing cycle reset on plan_percent_used")
	}
	wantReset := time.UnixMilli(1790685824000).UTC()
	if !reset.Equal(wantReset) {
		t.Fatalf("reset = %v, want %v", reset, wantReset)
	}
}

func TestFetchLivePlan_MonthlyHitWhenAutoOrAPIIs100(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"billingCycleEnd": "1790685824000",
			"planUsage": {
				"autoPercentUsed": 100,
				"apiPercentUsed": 12,
				"totalPercentUsed": 80
			}
		}`))
	}))
	defer ts.Close()

	path := filepath.Join(t.TempDir(), "cursor-status.json")
	if err := os.WriteFile(path, []byte(`{"model":{"display_name":"Auto"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	snap, err := New().Fetch(context.Background(), core.AccountConfig{
		ID:      "cursor",
		Token:   "test-jwt",
		BaseURL: ts.URL,
		ProviderPaths: map[string]string{
			"status_file": path,
			"state_db":    filepath.Join(t.TempDir(), "missing.vscdb"),
		},
	})
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	if snap.Status != core.StatusLimited {
		t.Fatalf("status = %q, want LIMITED when auto is 100%%", snap.Status)
	}
	if !strings.Contains(strings.ToLower(snap.Message), "monthly") {
		t.Fatalf("message = %q, want monthly usage hit", snap.Message)
	}
}

func TestEnrichSnapshots_UsesAuthFileHint(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer hinted-jwt" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/aiserver.v1.DashboardService/GetCurrentPeriodUsage" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"planUsage":{"totalPercentUsed":13.2,"autoPercentUsed":10.7,"apiPercentUsed":66.4}}`))
	}))
	defer ts.Close()

	authFile := filepath.Join(t.TempDir(), "auth.json")
	if err := os.WriteFile(authFile, []byte(`{"accessToken":"hinted-jwt"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	snaps := map[string]core.UsageSnapshot{
		"cursor-nurulz": {
			ProviderID: "cursor",
			AccountID:  "cursor-nurulz",
			Status:     core.StatusOK,
			Metrics:    map[string]core.Metric{},
		},
	}
	New().WithClient(NewClient(ts.URL, ts.Client())).EnrichSnapshots(context.Background(), []core.AccountConfig{{
		ID:       "cursor-nurulz",
		Provider: "cursor",
		ProviderPaths: map[string]string{
			"auth_file": authFile,
			"state_db":  filepath.Join(t.TempDir(), "missing.vscdb"),
		},
	}}, snaps)

	got := snaps["cursor-nurulz"]
	if metricUsed(t, got, "plan_percent_used") != 13.2 {
		t.Fatalf("included = %v, want 13.2 from auth.json token", metricUsed(t, got, "plan_percent_used"))
	}
	if metricUsed(t, got, "plan_api_percent_used") != 66.4 {
		t.Fatalf("api = %v, want 66.4", metricUsed(t, got, "plan_api_percent_used"))
	}
}

func TestEnrichSnapshots_FullFetchUpdatesPlanMetrics(t *testing.T) {
	ts := startNestedPlanUsageServer(t)
	defer ts.Close()

	snaps := map[string]core.UsageSnapshot{
		"cursor-nurulz": {
			ProviderID: "cursor",
			AccountID:  "cursor-nurulz",
			Status:     core.StatusOK,
			Metrics: map[string]core.Metric{
				"context_window": {
					Used:      core.Float64Ptr(24.5),
					Remaining: core.Float64Ptr(75.5),
					Limit:     core.Float64Ptr(100),
					Unit:      "%",
				},
			},
		},
	}
	New().WithClient(NewClient(ts.URL, ts.Client())).EnrichSnapshots(context.Background(), []core.AccountConfig{{
		ID:       "cursor-nurulz",
		Provider: "cursor",
		Token:    "test-jwt",
		ProviderPaths: map[string]string{
			"state_db": filepath.Join(t.TempDir(), "missing.vscdb"),
		},
	}}, snaps)

	got := snaps["cursor-nurulz"]
	if metricUsed(t, got, "plan_percent_used") != 7 {
		t.Fatalf("Included = %v, want 7", metricUsed(t, got, "plan_percent_used"))
	}
	if metricUsed(t, got, "plan_auto_percent_used") != 6 {
		t.Fatalf("Auto = %v, want 6", metricUsed(t, got, "plan_auto_percent_used"))
	}
	if metricUsed(t, got, "plan_api_percent_used") != 29 {
		t.Fatalf("API = %v, want 29", metricUsed(t, got, "plan_api_percent_used"))
	}
	if time.Since(got.Timestamp) > 5*time.Second {
		t.Fatalf("timestamp = %v, want within 5s of enrich refresh", got.Timestamp)
	}
}
