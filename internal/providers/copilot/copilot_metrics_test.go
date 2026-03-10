package copilot

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestReadSessions_EmitsModelTokenMetrics(t *testing.T) {
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
		"2026-02-20T01:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-20T01:00:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-20T01:00:02.000Z [INFO] CompactionProcessor: Utilization 1.4% (1800/128000 tokens) below threshold 80%",
		"2026-02-20T02:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-20T02:00:01.000Z [INFO] CompactionProcessor: Utilization 0.7% (900/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process-test.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, created, updated string) {
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
			`{"type":"session.model_change","timestamp":"` + created + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + created + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + created + `","data":{"turnId":"0"}}`,
			`{"type":"assistant.message","timestamp":"` + updated + `","data":{"content":"world","reasoningText":"r","toolRequests":[{"name":"read_file"}]}}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	mkSession("s1", "gpt-5-mini", "2026-02-20T01:00:00Z", "2026-02-20T01:10:00Z")
	mkSession("s2", "claude-sonnet-4.6", "2026-02-20T02:00:00Z", "2026-02-20T02:10:00Z")

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}

	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	if m := snap.Metrics["model_gpt_5_mini_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("model_gpt_5_mini_input_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["model_claude_sonnet_4_6_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("model_claude_sonnet_4_6_input_tokens missing/zero: %+v", m)
	}
	if _, ok := snap.DailySeries["tokens_gpt_5_mini"]; !ok {
		t.Fatal("missing tokens_gpt_5_mini series")
	}
	if m := snap.Metrics["cli_input_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("cli_input_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["client_owner_repo_total_tokens"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("client_owner_repo_total_tokens missing/zero: %+v", m)
	}
	if m := snap.Metrics["client_owner_repo_sessions"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("client_owner_repo_sessions = %+v, want 2", m)
	}
	if _, ok := snap.DailySeries["tokens_client_owner_repo"]; !ok {
		t.Fatal("missing tokens_client_owner_repo series")
	}
	if got := snap.Raw["client_usage"]; !strings.Contains(got, "owner/repo") {
		t.Fatalf("client_usage = %q, want owner/repo", got)
	}
	if m := snap.Metrics["messages_today"]; m.Used == nil || *m.Used <= 0 {
		t.Fatalf("messages_today missing/zero: %+v", m)
	}
	if m := snap.Metrics["tool_read_file"]; m.Used == nil || *m.Used != 2 {
		t.Fatalf("tool_read_file = %+v, want 2 calls", m)
	}
	if _, ok := snap.Metrics["context_window"]; !ok {
		t.Fatal("missing context_window metric")
	}
}

func TestReadLogs_UsesNewestTokenEntryByTimestamp(t *testing.T) {
	p := New()
	tmp := t.TempDir()
	copilotDir := filepath.Join(tmp, ".copilot")
	logDir := filepath.Join(copilotDir, "logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("mkdir logs: %v", err)
	}

	newer := strings.Join([]string{
		"2026-02-21T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-21T10:00:01.000Z [INFO] CompactionProcessor: Utilization 3.9% (5000/128000 tokens) below threshold 80%",
	}, "\n")
	older := strings.Join([]string{
		"2026-02-20T10:00:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-20T10:00:01.000Z [INFO] CompactionProcessor: Utilization 0.8% (1000/128000 tokens) below threshold 80%",
	}, "\n")
	// Lexicographic order is intentionally opposite to timestamp order.
	if err := os.WriteFile(filepath.Join(logDir, "a-new.log"), []byte(newer), 0o644); err != nil {
		t.Fatalf("write newer log: %v", err)
	}
	if err := os.WriteFile(filepath.Join(logDir, "z-old.log"), []byte(older), 0o644); err != nil {
		t.Fatalf("write older log: %v", err)
	}

	snap := &core.UsageSnapshot{
		Metrics: make(map[string]core.Metric),
		Resets:  make(map[string]time.Time),
		Raw:     make(map[string]string),
	}
	logs := p.readLogs(copilotDir, snap)

	if got := snap.Raw["context_window_tokens"]; got != "5000/128000" {
		t.Fatalf("context_window_tokens = %q, want %q", got, "5000/128000")
	}
	if got := logs.SessionTokens["s1"].Used; got != 5000 {
		t.Fatalf("session s1 used = %d, want 5000", got)
	}
}

func TestReadSessions_UsesLatestEventTimestampForRecency(t *testing.T) {
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
		"2026-02-21T13:05:00.000Z [INFO] Workspace initialized: s1 (checkpoints: 0)",
		"2026-02-21T13:05:01.000Z [INFO] CompactionProcessor: Utilization 1.0% (1200/128000 tokens) below threshold 80%",
		"2026-02-21T15:00:00.000Z [INFO] Workspace initialized: s2 (checkpoints: 0)",
		"2026-02-21T15:00:01.000Z [INFO] CompactionProcessor: Utilization 1.7% (2200/128000 tokens) below threshold 80%",
	}, "\n")
	if err := os.WriteFile(filepath.Join(logDir, "process.log"), []byte(logContent), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}

	mkSession := func(id, model, wsCreated, wsUpdated, evtTs string) {
		dir := filepath.Join(sessionDir, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", id, err)
		}
		ws := strings.Join([]string{
			"id: " + id,
			"repository: owner/repo",
			"branch: main",
			"created_at: " + wsCreated,
			"updated_at: " + wsUpdated,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "workspace.yaml"), []byte(ws), 0o644); err != nil {
			t.Fatalf("write workspace %s: %v", id, err)
		}
		events := strings.Join([]string{
			`{"type":"session.model_change","timestamp":"` + evtTs + `","data":{"newModel":"` + model + `"}}`,
			`{"type":"user.message","timestamp":"` + evtTs + `","data":{"content":"hello"}}`,
			`{"type":"assistant.turn_start","timestamp":"` + evtTs + `","data":{"turnId":"0"}}`,
		}, "\n")
		if err := os.WriteFile(filepath.Join(dir, "events.jsonl"), []byte(events), 0o644); err != nil {
			t.Fatalf("write events %s: %v", id, err)
		}
	}

	// Workspace metadata claims s1 is newer, but session events show s2 is latest.
	mkSession("s1", "model-s1", "2026-02-21T10:00:00Z", "2026-02-21T13:00:00Z", "2026-02-21T13:05:00Z")
	mkSession("s2", "model-s2", "2026-02-21T10:00:00Z", "2026-02-21T12:00:00Z", "2026-02-21T15:00:00Z")

	snap := &core.UsageSnapshot{
		Metrics:     make(map[string]core.Metric),
		Resets:      make(map[string]time.Time),
		Raw:         make(map[string]string),
		DailySeries: make(map[string][]core.TimePoint),
	}
	logs := p.readLogs(copilotDir, snap)
	p.readSessions(copilotDir, snap, logs)

	if got := snap.Raw["last_session_model"]; got != "model-s2" {
		t.Fatalf("last_session_model = %q, want model-s2", got)
	}
	if got := snap.Raw["last_session_tokens"]; got != "2200/128000" {
		t.Fatalf("last_session_tokens = %q, want 2200/128000", got)
	}
	if got := snap.Raw["last_session_time"]; got != "2026-02-21T15:00:01Z" {
		t.Fatalf("last_session_time = %q, want 2026-02-21T15:00:01Z", got)
	}
}

func TestSessionShutdownDataParsing(t *testing.T) {
	body := `{
		"shutdownType": "user_exit",
		"totalPremiumRequests": 12,
		"totalApiDurationMs": 45000,
		"sessionStartTime": "2026-02-24T10:00:00Z",
		"codeChanges": {"linesAdded": 150, "linesRemoved": 30, "filesModified": 5},
		"modelMetrics": {
			"claude-sonnet-4.5": {
				"requests": {"count": 10, "cost": 0.35},
				"usage": {"inputTokens": 52000, "outputTokens": 18000, "cacheReadTokens": 30000, "cacheWriteTokens": 5000}
			},
			"gpt-5-mini": {
				"requests": {"count": 2, "cost": 0.05},
				"usage": {"inputTokens": 3000, "outputTokens": 1000, "cacheReadTokens": 0, "cacheWriteTokens": 0}
			}
		}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "user_exit" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "user_exit")
	}
	if shutdown.TotalPremiumRequests != 12 {
		t.Errorf("TotalPremiumRequests = %d, want 12", shutdown.TotalPremiumRequests)
	}
	if shutdown.TotalAPIDurationMs != 45000 {
		t.Errorf("TotalAPIDurationMs = %d, want 45000", shutdown.TotalAPIDurationMs)
	}
	if shutdown.SessionStartTime != "2026-02-24T10:00:00Z" {
		t.Errorf("SessionStartTime = %q", shutdown.SessionStartTime)
	}
	if shutdown.CodeChanges.LinesAdded != 150 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 150", shutdown.CodeChanges.LinesAdded)
	}
	if shutdown.CodeChanges.LinesRemoved != 30 {
		t.Errorf("CodeChanges.LinesRemoved = %d, want 30", shutdown.CodeChanges.LinesRemoved)
	}
	if shutdown.CodeChanges.FilesModified != 5 {
		t.Errorf("CodeChanges.FilesModified = %d, want 5", shutdown.CodeChanges.FilesModified)
	}
	if len(shutdown.ModelMetrics) != 2 {
		t.Fatalf("expected 2 model metrics, got %d", len(shutdown.ModelMetrics))
	}

	claude := shutdown.ModelMetrics["claude-sonnet-4.5"]
	if claude.Requests.Count != 10 {
		t.Errorf("claude requests count = %d, want 10", claude.Requests.Count)
	}
	if claude.Requests.Cost != 0.35 {
		t.Errorf("claude requests cost = %f, want 0.35", claude.Requests.Cost)
	}
	if claude.Usage.InputTokens != 52000 {
		t.Errorf("claude input tokens = %f, want 52000", claude.Usage.InputTokens)
	}
	if claude.Usage.OutputTokens != 18000 {
		t.Errorf("claude output tokens = %f, want 18000", claude.Usage.OutputTokens)
	}
	if claude.Usage.CacheReadTokens != 30000 {
		t.Errorf("claude cache read tokens = %f, want 30000", claude.Usage.CacheReadTokens)
	}
	if claude.Usage.CacheWriteTokens != 5000 {
		t.Errorf("claude cache write tokens = %f, want 5000", claude.Usage.CacheWriteTokens)
	}

	gpt := shutdown.ModelMetrics["gpt-5-mini"]
	if gpt.Requests.Count != 2 {
		t.Errorf("gpt requests count = %d, want 2", gpt.Requests.Count)
	}
	if gpt.Requests.Cost != 0.05 {
		t.Errorf("gpt requests cost = %f, want 0.05", gpt.Requests.Cost)
	}
}

func TestSessionShutdownDataParsing_Empty(t *testing.T) {
	body := `{
		"shutdownType": "timeout",
		"totalPremiumRequests": 0,
		"totalApiDurationMs": 0,
		"codeChanges": {},
		"modelMetrics": {}
	}`

	var shutdown sessionShutdownData
	if err := unmarshalJSON(body, &shutdown); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if shutdown.ShutdownType != "timeout" {
		t.Errorf("ShutdownType = %q, want %q", shutdown.ShutdownType, "timeout")
	}
	if shutdown.TotalPremiumRequests != 0 {
		t.Errorf("TotalPremiumRequests = %d, want 0", shutdown.TotalPremiumRequests)
	}
	if shutdown.CodeChanges.LinesAdded != 0 {
		t.Errorf("CodeChanges.LinesAdded = %d, want 0", shutdown.CodeChanges.LinesAdded)
	}
	if len(shutdown.ModelMetrics) != 0 {
		t.Errorf("expected 0 model metrics, got %d", len(shutdown.ModelMetrics))
	}
}
