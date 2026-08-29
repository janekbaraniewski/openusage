package telemetry

import (
	"context"
	"testing"
	"time"
)

func TestLastEventTimesGroupsByProviderAccount(t *testing.T) {
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO usage_raw_events
			(raw_event_id, ingested_at, source_system, source_channel,
			 source_schema_version, source_payload, source_payload_hash)
		 VALUES ('raw-1', '2026-08-15T12:00:00Z', 'test', 'hook', '1', '{}', 'hash-1')`)
	if err != nil {
		t.Fatalf("insert raw: %v", err)
	}

	insert := func(id, provider, account, occurred string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO usage_events
				(event_id, occurred_at, provider_id, agent_name, account_id,
				 event_type, status, dedup_key, raw_event_id, normalization_version)
			 VALUES (?, ?, ?, 'test', ?, 'message_usage', 'ok', ?, 'raw-1', '1')`,
			id, occurred, provider, account, id)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	insert("e1", "codex", "default", "2026-08-15T10:00:00Z")
	insert("e2", "codex", "default", "2026-08-15T12:00:00Z")
	insert("e3", "claude_code", "default", "2026-08-15T11:00:00Z")
	if _, err := db.ExecContext(ctx,
		`INSERT INTO usage_events
			(event_id, occurred_at, provider_id, agent_name, account_id,
			 event_type, status, dedup_key, raw_event_id, normalization_version)
		 VALUES ('limit-1', '2026-08-15T23:00:00Z', 'openai', 'test', 'default',
			 'limit_snapshot', 'ok', 'limit-1', 'raw-1', '1')`); err != nil {
		t.Fatalf("insert limit snapshot: %v", err)
	}

	got, err := store.LastEventTimes(ctx)
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d keys, want 2: %v", len(got), got)
	}
	wantCodex := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	if !got["codex:default"].Equal(wantCodex) {
		t.Errorf("codex:default = %v, want %v", got["codex:default"], wantCodex)
	}
	wantClaude := time.Date(2026, 8, 15, 11, 0, 0, 0, time.UTC)
	if !got["claude_code:default"].Equal(wantClaude) {
		t.Errorf("claude_code:default = %v, want %v", got["claude_code:default"], wantClaude)
	}
}

func TestLastEventTimesEmptyStore(t *testing.T) {
	_, _, store := openUsageViewRawTestStore(t)
	got, err := store.LastEventTimes(context.Background())
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want empty", got)
	}
}

func TestLastEventTimesAliasesClaudeCodeHookEvents(t *testing.T) {
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()

	_, err := db.ExecContext(ctx,
		`INSERT INTO usage_raw_events
			(raw_event_id, ingested_at, source_system, source_channel,
			 source_schema_version, source_payload, source_payload_hash)
		 VALUES ('raw-1', '2026-08-15T12:00:00Z', 'test', 'hook', '1', '{}', 'hash-1')`)
	if err != nil {
		t.Fatalf("insert raw: %v", err)
	}

	insert := func(id, provider, agent, account, occurred string) {
		t.Helper()
		_, err := db.ExecContext(ctx,
			`INSERT INTO usage_events
				(event_id, occurred_at, provider_id, agent_name, account_id,
				 event_type, status, dedup_key, raw_event_id, normalization_version)
			 VALUES (?, ?, ?, ?, ?, 'message_usage', 'ok', ?, 'raw-1', '1')`,
			id, occurred, provider, agent, account, id)
		if err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// Claude Code hook events land under provider_id "anthropic" while the
	// credential poller registers the candidate as "claude_code". They must
	// collapse onto the poller's key or active selection never sees Claude.
	insert("hook-1", "anthropic", "claude_code", "claude-code", "2026-08-15T14:00:00Z")
	// A newer Anthropic API-key event from a different agent must NOT be
	// folded into the Claude Code candidate.
	insert("api-1", "anthropic", "some_other_agent", "claude-code", "2026-08-15T18:00:00Z")
	insert("codex-1", "codex", "codex_cli", "codex-cli", "2026-08-15T13:00:00Z")

	got, err := store.LastEventTimes(ctx)
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}

	wantHook := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	if !got["claude_code:claude-code"].Equal(wantHook) {
		t.Errorf("claude_code:claude-code = %v, want %v", got["claude_code:claude-code"], wantHook)
	}
	wantAPI := time.Date(2026, 8, 15, 18, 0, 0, 0, time.UTC)
	if !got["anthropic:claude-code"].Equal(wantAPI) {
		t.Errorf("anthropic:claude-code = %v, want %v", got["anthropic:claude-code"], wantAPI)
	}
	wantCodex := time.Date(2026, 8, 15, 13, 0, 0, 0, time.UTC)
	if !got["codex:codex-cli"].Equal(wantCodex) {
		t.Errorf("codex:codex-cli = %v, want %v", got["codex:codex-cli"], wantCodex)
	}
}

func TestLastEventTimesAliasKeepsLaterPollerKeyEvent(t *testing.T) {
	_, db, store := openUsageViewRawTestStore(t)
	ctx := context.Background()

	if _, err := db.ExecContext(ctx,
		`INSERT INTO usage_raw_events
			(raw_event_id, ingested_at, source_system, source_channel,
			 source_schema_version, source_payload, source_payload_hash)
		 VALUES ('raw-1', '2026-08-15T12:00:00Z', 'test', 'hook', '1', '{}', 'hash-1')`); err != nil {
		t.Fatalf("insert raw: %v", err)
	}

	insert := func(id, provider, agent, occurred string) {
		t.Helper()
		if _, err := db.ExecContext(ctx,
			`INSERT INTO usage_events
				(event_id, occurred_at, provider_id, agent_name, account_id,
				 event_type, status, dedup_key, raw_event_id, normalization_version)
			 VALUES (?, ?, ?, ?, 'claude-code', 'message_usage', 'ok', ?, 'raw-1', '1')`,
			id, occurred, provider, agent, id); err != nil {
			t.Fatalf("insert %s: %v", id, err)
		}
	}

	// Both spellings carry real activity; the merged key must take the max.
	insert("hook-1", "anthropic", "claude_code", "2026-08-15T14:00:00Z")
	insert("native-1", "claude_code", "claude_code", "2026-08-15T16:00:00Z")

	got, err := store.LastEventTimes(ctx)
	if err != nil {
		t.Fatalf("LastEventTimes: %v", err)
	}
	want := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	if !got["claude_code:claude-code"].Equal(want) {
		t.Errorf("claude_code:claude-code = %v, want %v", got["claude_code:claude-code"], want)
	}
}
