package copilot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestReadSessions_AccumulatesShutdownEvents(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-24T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-24T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-24T12:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-24T12:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSessionWithShutdown := func(id, created, updated string, shutdownJSON string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}
		events := strings.Join([]string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"claude-sonnet-4.5"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[]}}`,
			`{"type":"session.shutdown","timestamp":"` + updated + `","data":` + shutdownJSON + `}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	shutdown1 := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 8,
		"totalApiDurationMs": 30000,
		"sessionStartTime": "2026-02-24T10:00:00Z",
		"codeChanges": {"linesAdded": 100, "linesRemoved": 20, "filesModified": 3},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 6, "cost": 0.25},
				"usage": {"inputTokens": 40000, "outputTokens": 12000, "cacheReadTokens": 20000, "cacheWriteTokens": 3000}
			},
			"gpt-5-mini": {
				"requests": {"count": 2, "cost": 0.04},
				"usage": {"inputTokens": 2000, "outputTokens": 800, "cacheReadTokens": 0, "cacheWriteTokens": 0}
			}
		}
	}`

	shutdown2 := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 4,
		"totalApiDurationMs": 15000,
		"sessionStartTime": "2026-02-24T12:00:00Z",
		"codeChanges": {"linesAdded": 50, "linesRemoved": 10, "filesModified": 2},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 4, "cost": 0.10},
				"usage": {"inputTokens": 12000, "outputTokens": 6000, "cacheReadTokens": 10000, "cacheWriteTokens": 2000}
			}
		}
	}`

	mkSessionWithShutdown("s1", "2026-02-24T10:00:00Z", "2026-02-24T11:00:00Z", shutdown1)
	mkSessionWithShutdown("s2", "2026-02-24T12:00:00Z", "2026-02-24T13:00:00Z", shutdown2)

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify that the session data is still correctly parsed (existing behavior).
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("cli_messages = %+v, want 2", m)
	}

	// Verify total_sessions raw value accounts for both sessions.
	if got := snap.Raw["total_sessions"]; got != "2" {
		t.Fatalf("total_sessions = %q, want 2", got)
	}
}

func TestSessionShutdownDataParsing_NoModelMetrics(t *testing.T) {
	body := `{
		"shutdownType": "crash",
		"totalPremiumRequests": 3,
		"totalApiDurationMs": 5000,
		"codeChanges": {"linesAdded": 10, "linesRemoved": 2, "filesModified": 1}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "crash" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "crash")
	}
	if shutdown.TotalPremiumRequests != 3 {
		t.Errorf("TotalPremiumRequests = %d, want 3", shutdown.TotalPremiumRequests)
	}
	if shutdown.CodeChanges.LinesAdded != 10 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 10", shutdown.CodeChanges.LinesAdded)
	}
	if shutdown.ModelMetrics != nil {
		t.Errorf("expected nil ModelMetrics, got %v", shutdown.ModelMetrics)
	}
}

func TestAssistantUsageDataParsing(t *testing.T) {
	body := `{
		"model": "claude-sonnet-4.5",
		"inputTokens": 5200,
		"outputTokens": 1800,
		"cacheReadTokens": 3000,
		"cacheWriteTokens": 500,
		"cost": 0.042,
		"duration": 2500,
		"quotaSnapshots": {
			"premium_interactions": {
				"entitlementRequests": 300,
				"usedRequests": 158,
				"remainingPercentage": 47.3,
				"resetDate": "2026-03-01T00:00:00Z"
			}
		}
	}`

	var usage assistantUsageData
	if err := unmarshalJSON(body, &usage); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if usage.Model != "claude-sonnet-4.5" {
		t.Errorf("Model = %q, want %q", usage.Model, "claude-sonnet-4.5")
	}
	if usage.InputTokens != 5200 {
		t.Errorf("InputTokens = %f, want 5200", usage.InputTokens)
	}
	if usage.OutputTokens != 1800 {
		t.Errorf("OutputTokens = %f, want 1800", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 3000 {
		t.Errorf("CacheReadTokens = %f, want 3000", usage.CacheReadTokens)
	}
	if usage.CacheWriteTokens != 500 {
		t.Errorf("CacheWriteTokens = %f, want 500", usage.CacheWriteTokens)
	}
	if usage.Cost != 0.042 {
		t.Errorf("Cost = %f, want 0.042", usage.Cost)
	}
	if usage.Duration != 2500 {
		t.Errorf("Duration = %d, want 2500", usage.Duration)
	}
	if len(usage.QuotaSnapshots) != 1 {
		t.Fatalf("expected 1 quota snapshot, got %d", len(usage.QuotaSnapshots))
	}

	premium := usage.QuotaSnapshots["premium_interactions"]
	if premium.EntitlementRequests != 300 {
		t.Errorf("EntitlementRequests = %d, want 300", premium.EntitlementRequests)
	}
	if premium.UsedRequests != 158 {
		t.Errorf("UsedRequests = %d, want 158", premium.UsedRequests)
	}
	if premium.RemainingPercentage != 47.3 {
		t.Errorf("RemainingPercentage = %f, want 47.3", premium.RemainingPercentage)
	}
	if premium.ResetDate != "2026-03-01T00:00:00Z" {
		t.Errorf("ResetDate = %q, want %q", premium.ResetDate, "2026-03-01T00:00:00Z")
	}
}

func TestAssistantUsageDataParsing_NoQuota(t *testing.T) {
	body := `{
		"model": "gpt-5-mini",
		"inputTokens": 1000,
		"outputTokens": 500,
		"cacheReadTokens": 0,
		"cacheWriteTokens": 0,
		"cost": 0.01,
		"duration": 800
	}`

	var usage assistantUsageData
	if err := unmarshalJSON(body, &usage); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if usage.Model != "gpt-5-mini" {
		t.Errorf("Model = %q", usage.Model)
	}
	if usage.InputTokens != 1000 {
		t.Errorf("InputTokens = %f, want 1000", usage.InputTokens)
	}
	if usage.OutputTokens != 500 {
		t.Errorf("OutputTokens = %f, want 500", usage.OutputTokens)
	}
	if len(usage.QuotaSnapshots) != 0 {
		t.Errorf("expected 0 quota snapshots, got %d", len(usage.QuotaSnapshots))
	}
}

func TestReadSessions_AccumulatesUsageEvents(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-25T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-25T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSessionWithUsage := func(id, created, updated string, usageEvents []string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}

		baseEvents := []string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"claude-sonnet-4.5"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[]}}`,
		}
		allEvents := append(baseEvents, usageEvents...)
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(allEvents, "\n")), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	usageEvent1 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:05:00Z","data":{` +
		`"model":"claude-sonnet-4.5","inputTokens":5200,"outputTokens":1800,` +
		`"cacheReadTokens":3000,"cacheWriteTokens":500,"cost":0.042,"duration":2500,` +
		`"quotaSnapshots":{"premium_interactions":{"entitlementRequests":300,"usedRequests":150,"remainingPercentage":50.0,"resetDate":"2026-03-01T00:00:00Z"}}}}`

	usageEvent2 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:10:00Z","data":{` +
		`"model":"claude-sonnet-4.5","inputTokens":3000,"outputTokens":1200,` +
		`"cacheReadTokens":2000,"cacheWriteTokens":300,"cost":0.028,"duration":1800,` +
		`"quotaSnapshots":{"premium_interactions":{"entitlementRequests":300,"usedRequests":152,"remainingPercentage":49.3,"resetDate":"2026-03-01T00:00:00Z"}}}}`

	usageEvent3 := `{"type":"assistant.usage","timestamp":"2026-02-25T10:15:00Z","data":{` +
		`"model":"gpt-5-mini","inputTokens":1000,"outputTokens":500,` +
		`"cacheReadTokens":0,"cacheWriteTokens":0,"cost":0.01,"duration":800}}`

	mkSessionWithUsage("s1", "2026-02-25T10:00:00Z", "2026-02-25T10:20:00Z",
		[]string{usageEvent1, usageEvent2, usageEvent3})

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify that existing session behavior still works.
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 1 {
		t.Fatalf("cli_messages = %+v, want 1", m)
	}
	if got := snap.Raw["total_sessions"]; got != "1" {
		t.Fatalf("total_sessions = %q, want 1", got)
	}

	// The usage data is accumulated internally but not yet emitted as metrics
	// (that is Task 5). This test verifies the parsing does not break existing
	// behavior and that the events are parsed without errors.
	// We verify by checking the session still has correct model and timestamps.
	if got := snap.Raw["last_session_model"]; got != "claude-sonnet-4.5" {
		t.Fatalf("last_session_model = %q, want claude-sonnet-4.5", got)
	}
}

func TestReadSessions_UsageEventsMultipleSessions(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-25T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-25T10:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-25T14:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-25T14:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, created, updated string, usageEvents []string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + created,
			"updated_at: " + updated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}

		baseEvents := []string{
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"reply","reasoningText":"","toolRequests":[]}}`,
		}
		allEvents := append(baseEvents, usageEvents...)
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(strings.Join(allEvents, "\n")), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	s1Usage := []string{
		`{"type":"assistant.usage","timestamp":"2026-02-25T10:05:00Z","data":{"model":"claude-sonnet-4.5","inputTokens":5200,"outputTokens":1800,"cacheReadTokens":3000,"cacheWriteTokens":500,"cost":0.042,"duration":2500}}`,
		`{"type":"assistant.usage","timestamp":"2026-02-25T10:10:00Z","data":{"model":"claude-sonnet-4.5","inputTokens":3000,"outputTokens":1200,"cacheReadTokens":2000,"cacheWriteTokens":300,"cost":0.028,"duration":1800}}`,
	}

	s2Usage := []string{
		`{"type":"assistant.usage","timestamp":"2026-02-25T14:05:00Z","data":{"model":"gpt-5-mini","inputTokens":1000,"outputTokens":500,"cacheReadTokens":0,"cacheWriteTokens":0,"cost":0.01,"duration":800}}`,
	}

	mkSession("s1", "claude-sonnet-4.5", "2026-02-25T10:00:00Z", "2026-02-25T10:20:00Z", s1Usage)
	mkSession("s2", "gpt-5-mini", "2026-02-25T14:00:00Z", "2026-02-25T14:10:00Z", s2Usage)

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	// Verify existing behavior is preserved.
	if m := snap.Metrics["cli_messages"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("cli_messages = %+v, want 2", m)
	}
	if got := snap.Raw["total_sessions"]; got != "2" {
		t.Fatalf("total_sessions = %q, want 2", got)
	}

	// The latest session (s2 at 14:10) should be shown as last.
	if got := snap.Raw["last_session_model"]; got != "gpt-5-mini" {
		t.Fatalf("last_session_model = %q, want gpt-5-mini", got)
	}
}

func TestExtractCopilotToolPathsAndLanguage(t *testing.T) {
	raw := json.RawMessage(`{"name":"read_file","args":{"path":"internal/providers/copilot/copilot.go"}}`)
	paths := extractCopilotToolPaths(raw)
	if len(paths) != 1 || paths[0] != "internal/providers/copilot/copilot.go" {
		t.Fatalf("extractCopilotToolPaths = %v", paths)
	}
	if lang := inferCopilotLanguageFromPath(paths[0]); lang != "go" {
		t.Fatalf("inferCopilotLanguageFromPath = %q, want go", lang)
	}
}

func TestReadSessions_ExtractsLanguageAndCodeStatsMetrics(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	sessionDir := filepath.Join(copilotDir, "session-state")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir sessions: %v", err)
	}

	logContent := strings.Join([]string{
		"2026-02-25T14:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-25T14:00:01.000Z [INFO] CompactionProcessor: Utilization 1.1% (1400/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	s1Dir := filepath.Join(sessionDir, "s1")
	if err := os.MkdirAll(s1Dir, 0o755); err != nil {
		t.Fatalf("mkdir s1: %v", err)
	}
	ws := strings.Join([]string{
		"id: s1",
		"repository: owner/repo",
		"branch: main",
		"created_at: 2026-02-25T14:00:00Z",
		"updated_at: 2026-02-25T14:10:00Z",
	}, "\n")
	if err := os.WriteFile(filepath.Join(s1Dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
		t.Fatalf("write workspace: %v", err)
	}

	events := strings.Join([]string{
		`{"type":"session.model_change","timestamp":"2026-02-25T14:00:00Z","data":{"newModel":"claude-sonnet-4.6"}}`,
		`{"type":"user.message","timestamp":"2026-02-25T14:00:01Z","data":{"content":"patch code"}}`,
		`{"type":"assistant.turn_start","timestamp":"2026-02-25T14:00:02Z","data":{"turnId":"0"}}`,
		`{"type":"assistant.message","timestamp":"2026-02-25T14:00:03Z","data":{"content":"done","reasoningText":"","toolRequests":[{"name":"read_file","args":{"path":"internal/providers/copilot/copilot.go"}},{"name":"edit_file","args":{"filePath":"internal/providers/copilot/widget.go","old_string":"a\nb","new_string":"a\nb\nc"}},{"name":"run_terminal","args":{"command":"git commit -m \"copilot metrics\""}}]}}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(s1Dir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	if m := snap.Metrics["lang_go"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("lang_go missing/zero: %+v", m)
	}
	if m := snap.Metrics["composer_lines_added"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("composer_lines_added missing/zero: %+v", m)
	}
	if m := snap.Metrics["composer_lines_removed"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("composer_lines_removed missing/zero: %+v", m)
	}
	if m := snap.Metrics["composer_files_changed"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("composer_files_changed missing/zero: %+v", m)
	}
	if m := snap.Metrics["scored_commits"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("scored_commits missing/zero: %+v", m)
	}
	if m := snap.Metrics["total_prompts"]; m.Used == nil || *m.Used != 1 {
		t.Fatalf("total_prompts = %+v, want 1", m)
	}
	if m := snap.Metrics["tool_calls_total"]; m.Used == nil || *m.Used != 3 {
		t.Fatalf("tool_calls_total = %+v, want 3", m)
	}
}

func TestDetectCopilotVersion_FallbackToStandalone(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh: unknown command copilot" >&2
  exit 1
fi
exit 1
`)
	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 1.2.3"
  exit 0
fi
exit 1
`)

	version, source, err := detectCopilotVersion(context.Background(), ghBin, copilotBin)
	if err != nil {
		t.Fatalf("detectCopilotVersion() error: %v", err)
	}
	if version != "copilot 1.2.3" {
		t.Fatalf("version = %q, want %q", version, "copilot 1.2.3")
	}
	if source != "copilot" {
		t.Fatalf("source = %q, want %q", source, "copilot")
	}
}

func TestFetch_FallsBackToStandaloneCopilotWhenGHCopilotUnavailable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	ghBin := writeTestExe(t, tmp, "gh", `
if [ "$1" = "copilot" ] && [ "$2" = "--version" ]; then
  echo "gh: unknown command copilot" >&2
  exit 1
fi
if [ "$1" = "auth" ] && [ "$2" = "status" ]; then
  echo "Logged in to github.com as octocat"
  exit 0
fi
if [ "$1" = "api" ]; then
  endpoint=""
  for arg in "$@"; do endpoint="$arg"; done
  case "$endpoint" in
    "/user")
      echo '{"login":"octocat","name":"Octo Cat","plan":{"name":"free"}}'
      exit 0
      ;;
    "/copilot_internal/user")
      echo '{"login":"octocat","access_type_sku":"copilot_pro","copilot_plan":"individual","chat_enabled":true,"is_mcp_enabled":false,"organization_login_list":[],"organization_list":[]}'
      exit 0
      ;;
    "/rate_limit")
      echo '{"resources":{"core":{"limit":5000,"remaining":4999,"reset":2000000000,"used":1}}}'
      exit 0
      ;;
  esac
fi
echo "unsupported gh args: $*" >&2
exit 1
`)

	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 1.2.3"
  exit 0
fi
exit 1
`)

	p := New()
	snap, err := p.Fetch(context.Background(), testCopilotAccount(ghBin, configDir, copilotBin))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status == core.StatusError || snap.Status == core.StatusAuth {
		t.Fatalf("Status = %q, want non-error/auth fallback", snap.Status)
	}
	if snap.Raw["copilot_version"] != "copilot 1.2.3" {
		t.Fatalf("copilot_version = %q, want %q", snap.Raw["copilot_version"], "copilot 1.2.3")
	}
	if snap.Raw["copilot_version_source"] != "copilot" {
		t.Fatalf("copilot_version_source = %q, want %q", snap.Raw["copilot_version_source"], "copilot")
	}
	if !strings.Contains(snap.Raw["auth_status"], "Logged in") {
		t.Fatalf("auth_status = %q, want GitHub auth output", snap.Raw["auth_status"])
	}
	if snap.Raw["github_login"] != "octocat" {
		t.Fatalf("github_login = %q, want %q", snap.Raw["github_login"], "octocat")
	}
}

func TestFetch_StandaloneCopilotWithoutGH(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses shell scripts")
	}

	tmp := t.TempDir()
	configDir := filepath.Join(t.TempDir(), ".copilot")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}

	copilotBin := writeTestExe(t, tmp, "copilot", `
if [ "$1" = "--version" ]; then
  echo "copilot 2.0.0"
  exit 0
fi
exit 1
`)
	t.Setenv("PATH", tmp)

	p := New()
	snap, err := p.Fetch(context.Background(), testCopilotAccount(copilotBin, configDir, ""))
	if err != nil {
		t.Fatalf("Fetch() error: %v", err)
	}
	if snap.Status != core.StatusOK {
		t.Fatalf("Status = %q, want %q", snap.Status, core.StatusOK)
	}
	if snap.Raw["copilot_version"] != "copilot 2.0.0" {
		t.Fatalf("copilot_version = %q, want %q", snap.Raw["copilot_version"], "copilot 2.0.0")
	}
	if snap.Raw["copilot_version_source"] != "copilot" {
		t.Fatalf("copilot_version_source = %q, want %q", snap.Raw["copilot_version_source"], "copilot")
	}
	if !strings.Contains(snap.Raw["auth_status"], "skipped GitHub API checks") {
		t.Fatalf("auth_status = %q, want skipped GH API message", snap.Raw["auth_status"])
	}
}

func writeTestExe(t *testing.T, dir, name, body string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := "#!/bin/sh\n" + strings.TrimSpace(body) + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", name, err)
	}
	return path
}

func unmarshalJSON(s string, v interface{}) error {
	return json.Unmarshal([]byte(s), v)
}

func boolPtr(v bool) *bool { return &v }
