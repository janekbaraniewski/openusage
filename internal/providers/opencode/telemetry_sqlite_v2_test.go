package opencode

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func newSessionMessageTestDB(t *testing.T, rows ...string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "opencode.db")
	stmts := []string{
		`CREATE TABLE session_v2 (id TEXT PRIMARY KEY, directory TEXT NOT NULL);`,
		`CREATE TABLE session_message (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, type TEXT NOT NULL, seq INTEGER NOT NULL, time_created INTEGER NOT NULL, time_updated INTEGER NOT NULL, data TEXT NOT NULL);`,
		`INSERT INTO session_v2 (id, directory) VALUES ('ses_1', '/home/user/projects/demo');`,
	}
	stmts = append(stmts, rows...)
	execSQL(t, path, stmts...)
	return path
}

func int64Value(p *int64) int64 {
	if p == nil {
		return -1
	}
	return *p
}

func sessionMessageInsert(id, typ, data string) string {
	return `INSERT INTO session_message (id, session_id, type, seq, time_created, time_updated, data) VALUES ('` +
		id + `', 'ses_1', '` + typ + `', 1, 1794843823000, 1794843826000, '` + data + `');`
}

const assistantWithUsage = `{"time":{"created":1794843823376,"completed":1794843825902},"agent":"build","model":{"id":"claude-haiku-5-5","providerID":"anthropic","variant":"default"},"finish":"tool-calls","cost":0.5,"tokens":{"input":4,"output":228,"reasoning":52,"cache":{"read":100,"write":7897}}}`

func TestCollectSQLiteV2Events(t *testing.T) {
	tests := []struct {
		name      string
		rows      []string
		wantCount int
		check     func(t *testing.T, ev shared.TelemetryEvent)
	}{
		{
			name:      "assistant row with usage becomes one event",
			rows:      []string{sessionMessageInsert("msg_a", "assistant", assistantWithUsage)},
			wantCount: 1,
			check: func(t *testing.T, ev shared.TelemetryEvent) {
				if ev.MessageID != "msg_a" {
					t.Errorf("MessageID = %q, want msg_a", ev.MessageID)
				}
				if ev.SessionID != "ses_1" {
					t.Errorf("SessionID = %q, want ses_1", ev.SessionID)
				}
				if ev.ProviderID != "anthropic" {
					t.Errorf("ProviderID = %q, want anthropic", ev.ProviderID)
				}
				if ev.ModelRaw != "claude-haiku-5-5" {
					t.Errorf("ModelRaw = %q, want claude-haiku-5-5", ev.ModelRaw)
				}
				if ev.WorkspaceID != "demo" {
					t.Errorf("WorkspaceID = %q, want demo", ev.WorkspaceID)
				}
				if got := int64Value(ev.TokenUsage.InputTokens); got != 4 {
					t.Errorf("InputTokens = %d, want 4", got)
				}
				if got := int64Value(ev.TokenUsage.OutputTokens); got != 228 {
					t.Errorf("OutputTokens = %d, want 228", got)
				}
				if got := int64Value(ev.TokenUsage.CacheReadTokens); got != 100 {
					t.Errorf("CacheReadTokens = %d, want 100", got)
				}
				if got := int64Value(ev.TokenUsage.CacheWriteTokens); got != 7897 {
					t.Errorf("CacheWriteTokens = %d, want 7897", got)
				}
				if ev.TokenUsage.CostUSD == nil || *ev.TokenUsage.CostUSD != 0.5 {
					t.Errorf("CostUSD = %v, want 0.5", ev.TokenUsage.CostUSD)
				}
				if got := ev.Payload["source"].(map[string]any)["table"]; got != "session_message" {
					t.Errorf("source table = %v, want session_message", got)
				}
			},
		},
		{
			name: "user and idle rows are skipped",
			rows: []string{
				sessionMessageInsert("msg_u", "user", `{"time":{"created":1},"text":"hi","files":[],"agents":[]}`),
				sessionMessageInsert("idle_1", "idle", `{"time":{"created":1},"outcome":"succeeded"}`),
			},
			wantCount: 0,
		},
		{
			name:      "assistant row without usage is skipped",
			rows:      []string{sessionMessageInsert("msg_n", "assistant", `{"time":{"created":1},"model":{"id":"claude-haiku-5-5","providerID":"anthropic"}}`)},
			wantCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := newSessionMessageTestDB(t, tt.rows...)
			db, err := openOpenCodeSQLiteReadOnly(path)
			if err != nil {
				t.Fatalf("open db: %v", err)
			}
			defer db.Close()
			events, err := collectSQLiteV2Events(context.Background(), db, path)
			if err != nil {
				t.Fatalf("collectSQLiteV2Events: %v", err)
			}
			if len(events) != tt.wantCount {
				t.Fatalf("got %d events, want %d", len(events), tt.wantCount)
			}
			if tt.check != nil && len(events) == 1 {
				tt.check(t, events[0])
			}
		})
	}
}

func TestCollectTelemetryFromSQLiteSince_RoutesToSessionMessage(t *testing.T) {
	path := newSessionMessageTestDB(t, sessionMessageInsert("msg_a", "assistant", assistantWithUsage))

	res, err := collectTelemetryFromSQLiteSince(context.Background(), path, nil)
	if err != nil {
		t.Fatalf("collectTelemetryFromSQLiteSince: %v", err)
	}
	if len(res.Events) != 1 {
		t.Fatalf("got %d events, want 1", len(res.Events))
	}
	if res.HasWatermark {
		t.Errorf("HasWatermark = true, want false for the session_message schema")
	}
}

func TestNormalizeSessionMessageModel(t *testing.T) {
	tests := []struct {
		name    string
		payload map[string]any
		wantID  any
	}{
		{
			name:    "copies model id into modelID",
			payload: map[string]any{"model": map[string]any{"id": "claude-haiku-5-5"}},
			wantID:  "claude-haiku-5-5",
		},
		{
			name:    "keeps an existing modelID",
			payload: map[string]any{"model": map[string]any{"id": "new", "modelID": "old"}},
			wantID:  "old",
		},
		{
			name:    "no model is a no-op",
			payload: map[string]any{"time": map[string]any{"created": 1}},
			wantID:  nil,
		},
		{
			name:    "non-object model is a no-op",
			payload: map[string]any{"model": "claude-haiku-5-5"},
			wantID:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			normalizeSessionMessageModel(tt.payload)
			got := any(nil)
			if m, ok := tt.payload["model"].(map[string]any); ok {
				got = m["modelID"]
			}
			if got != tt.wantID {
				t.Errorf("modelID = %v, want %v", got, tt.wantID)
			}
		})
	}
}
