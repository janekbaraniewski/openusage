package dsh

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/providers/shared"
)

func collectOptions(sessionsDir string) shared.TelemetryCollectOptions {
	return shared.TelemetryCollectOptions{
		Paths: map[string]string{PathHintSessionsDirKey: sessionsDir},
	}
}

func eventsByID(t *testing.T, events []shared.TelemetryEvent) map[string]shared.TelemetryEvent {
	t.Helper()
	out := make(map[string]shared.TelemetryEvent, len(events))
	for _, ev := range events {
		id := ev.MessageID
		if ev.EventType == shared.TelemetryEventTypeToolUsage {
			id = ev.ToolCallID
		}
		if id == "" {
			t.Fatalf("event without a stable id: %+v", ev)
		}
		if _, dup := out[id]; dup {
			t.Fatalf("duplicate event id %q", id)
		}
		out[id] = ev
	}
	return out
}

func TestProvider_ImplementsTelemetrySource(t *testing.T) {
	var source shared.TelemetrySource = New()
	if source.System() != ID {
		t.Errorf("System = %q, want %q", source.System(), ID)
	}
	if _, err := source.ParseHookPayload([]byte(`{}`), shared.TelemetryCollectOptions{}); !errors.Is(err, shared.ErrHookUnsupported) {
		t.Errorf("ParseHookPayload err = %v, want ErrHookUnsupported", err)
	}
}

func TestProvider_DefaultCollectOptions(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)

	opts := New().DefaultCollectOptions()
	if got, want := opts.Paths[PathHintSessionsDirKey], filepath.Join(home, ".dsh", "sessions"); got != want {
		t.Errorf("sessions_dir = %q, want %q", got, want)
	}

	relocated := t.TempDir()
	t.Setenv(homeEnvVar, relocated)
	opts = New().DefaultCollectOptions()
	if got, want := opts.Paths[PathHintSessionsDirKey], filepath.Join(relocated, "sessions"); got != want {
		t.Errorf("sessions_dir with DSH_HOME = %q, want %q", got, want)
	}
}

func TestCollect_MissingDir(t *testing.T) {
	setHome(t, t.TempDir())
	events, err := New().Collect(context.Background(), collectOptions(filepath.Join(t.TempDir(), "missing")))
	if err != nil || len(events) != 0 {
		t.Errorf("Collect = (%d events, %v), want (0, nil)", len(events), err)
	}
}

func TestCollect_EmitsMessageAndToolEvents(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	writeFixtureSessions(t, root)

	events, err := New().Collect(context.Background(), collectOptions(root))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(events) != 6 {
		t.Fatalf("len(events) = %d, want 6 (4 messages + 2 tool calls)", len(events))
	}
	byID := eventsByID(t, events)

	for _, ev := range events {
		if ev.SchemaVersion != telemetrySchemaVersion || ev.Channel != shared.TelemetryChannelJSONL {
			t.Errorf("%s: schema/channel = (%q, %q)", ev.MessageID+ev.ToolCallID, ev.SchemaVersion, ev.Channel)
		}
		if ev.AccountID != DefaultAccountID || ev.AgentName != "dsh" {
			t.Errorf("%s: account/agent = (%q, %q)", ev.MessageID+ev.ToolCallID, ev.AccountID, ev.AgentName)
		}
		if ev.OccurredAt.IsZero() {
			t.Errorf("%s: OccurredAt is zero", ev.MessageID+ev.ToolCallID)
		}
		if ev.CostUSD != nil {
			t.Errorf("%s: CostUSD = %v, want none", ev.MessageID+ev.ToolCallID, *ev.CostUSD)
		}
		if ev.Payload["client"] != "dsh" {
			t.Errorf("%s: payload client = %v", ev.MessageID+ev.ToolCallID, ev.Payload["client"])
		}
	}

	// Usage sent to the DeepSeek platform is stored under the deepseek
	// provider so it reaches that tile.
	m1 := byID["sess-1:m1"]
	if m1.EventType != shared.TelemetryEventTypeMessageUsage || m1.ProviderID != "deepseek" {
		t.Errorf("m1 type/provider = (%q, %q), want (message_usage, deepseek)", m1.EventType, m1.ProviderID)
	}
	if m1.ModelRaw != "deepseek-flash" || m1.SessionID != "sess-1" || m1.WorkspaceID != "alpha" || m1.TurnID != "sess-1:turn:0" {
		t.Errorf("m1 = (%q, %q, %q, %q)", m1.ModelRaw, m1.SessionID, m1.WorkspaceID, m1.TurnID)
	}
	if *m1.InputTokens != 100 || *m1.OutputTokens != 50 || *m1.CacheReadTokens != 1000 ||
		*m1.CacheWriteTokens != 0 || *m1.ReasoningTokens != 0 || *m1.TotalTokens != 1150 || *m1.Requests != 1 {
		t.Errorf("m1 tokens = (%d,%d,%d,%d,%d,%d,%d)", *m1.InputTokens, *m1.OutputTokens, *m1.CacheReadTokens,
			*m1.CacheWriteTokens, *m1.ReasoningTokens, *m1.TotalTokens, *m1.Requests)
	}
	if !m1.OccurredAt.Equal(time.UnixMilli(fixtureDay1 + 1000).UTC()) {
		t.Errorf("m1.OccurredAt = %v", m1.OccurredAt)
	}
	if m1.Payload["upstream_provider"] != "deepseek" {
		t.Errorf("m1 upstream_provider = %v, want deepseek", m1.Payload["upstream_provider"])
	}

	// A DeepSeek model hosted elsewhere stays on the dsh tile, tagged with
	// its host.
	m3 := byID["session-2:m3"]
	if m3.ProviderID != ID || m3.ModelRaw != "deepseek/deepseek-v4.1-flash" || m3.Payload["upstream_provider"] != "command_code" {
		t.Errorf("m3 = (%q, %q, %v), want (dsh, deepseek/deepseek-v4.1-flash, command_code)",
			m3.ProviderID, m3.ModelRaw, m3.Payload["upstream_provider"])
	}
	if *m3.ReasoningTokens != 80 || *m3.TotalTokens != 600 {
		t.Errorf("m3 reasoning/total = (%d,%d), want (80,600)", *m3.ReasoningTokens, *m3.TotalTokens)
	}
	if m4 := byID["session-2:m4"]; m4.ProviderID != ID || m4.Payload["upstream_provider"] != "zai" {
		t.Errorf("m4 = (%q, %v), want (dsh, zai)", m4.ProviderID, m4.Payload["upstream_provider"])
	}

	// Tool calls follow the route that was in effect when they ran.
	call1 := byID["call-1"]
	if call1.EventType != shared.TelemetryEventTypeToolUsage || call1.ProviderID != "deepseek" || call1.ToolName != "read" {
		t.Errorf("call-1 = (%q, %q, %q)", call1.EventType, call1.ProviderID, call1.ToolName)
	}
	if call1.Status != shared.TelemetryStatusOK || call1.Payload["file"] != "/work/alpha/main.go" || call1.SessionID != "sess-1" {
		t.Errorf("call-1 status/file/session = (%q, %v, %q)", call1.Status, call1.Payload["file"], call1.SessionID)
	}
	if call1.InputTokens != nil || call1.TotalTokens != nil || *call1.Requests != 1 {
		t.Errorf("call-1 should carry a request count and no tokens")
	}
	call2 := byID["call-2"]
	if call2.ProviderID != ID || call2.ToolName != "bash" || call2.Status != shared.TelemetryStatusError || call2.WorkspaceID != "beta" {
		t.Errorf("call-2 = (%q, %q, %q, %q)", call2.ProviderID, call2.ToolName, call2.Status, call2.WorkspaceID)
	}
	if _, ok := call2.Payload["file"]; ok {
		t.Errorf("call-2 should have no file, got %v", call2.Payload["file"])
	}
}

func TestCollect_AccountOverride(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	writeFixtureSessions(t, root)

	opts := collectOptions(root)
	opts.Paths["account_id"] = "dsh-work"
	events, err := New().Collect(context.Background(), opts)
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	for _, ev := range events {
		if ev.AccountID != "dsh-work" {
			t.Fatalf("AccountID = %q, want dsh-work", ev.AccountID)
		}
	}
}

func TestCollect_OnlyChangedSessionsAreReEmitted(t *testing.T) {
	setHome(t, t.TempDir())
	root := t.TempDir()
	writeFixtureSessions(t, root)
	p := New()
	ctx := context.Background()

	first, err := p.Collect(ctx, collectOptions(root))
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	firstByID := eventsByID(t, first)

	again, err := p.Collect(ctx, collectOptions(root))
	if err != nil {
		t.Fatalf("second Collect: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Collect returned %d events, want 0", len(again))
	}

	// sess-1 gains a message; its log is rewritten with a new size.
	writeSession(t, root, "proj-alpha", "sess-1",
		headerLine("sess-1", fixtureDay1, "/work/alpha", false),
		contextLine(1, fixtureDay1, "deepseek-official", "deepseek-flash"),
		messageLine(2, fixtureDay1+1000, "m1", 0, usageJSON(100, 50, 1000)),
		toolCallLine(3, fixtureDay1+1100, "call-1", "read", `{"file_path":"/work/alpha/main.go"}`),
		toolResultLine(4, fixtureDay1+1200, "call-1", false),
		messageLine(5, fixtureDay2+1000, "m2", 1, usageJSON(200, 100, 2000)),
		messageLine(6, fixtureDay2+2000, "m5", 2, usageJSON(1, 1, 1)),
	)

	third, err := p.Collect(ctx, collectOptions(root))
	if err != nil {
		t.Fatalf("third Collect: %v", err)
	}
	thirdByID := eventsByID(t, third)
	if len(thirdByID) != 4 {
		t.Fatalf("third Collect returned %d events, want sess-1's 4", len(thirdByID))
	}
	if _, ok := thirdByID["sess-1:m5"]; !ok {
		t.Error("new message sess-1:m5 missing")
	}
	// Re-emitted events keep the ids and figures they had, so ingest
	// deduplicates them.
	for id, ev := range thirdByID {
		if ev.SessionID != "sess-1" {
			t.Errorf("%s: unchanged session %q re-emitted", id, ev.SessionID)
		}
		before, ok := firstByID[id]
		if !ok {
			continue
		}
		if !before.OccurredAt.Equal(ev.OccurredAt) || before.ProviderID != ev.ProviderID || before.TurnID != ev.TurnID {
			t.Errorf("%s changed between collections", id)
		}
	}
}

func TestSessionCache_ReusesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	writeFixtureSessions(t, root)
	var cache sessionCache
	ctx := context.Background()

	first, err := cache.load(ctx, root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("len(first) = %d, want 3", len(first))
	}
	second, err := cache.load(ctx, root)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	for i := range first {
		if first[i].session != second[i].session {
			t.Errorf("%s was re-read although unchanged", first[i].path)
		}
	}

	// A moved mtime invalidates one entry; a deleted log is forgotten.
	touched := first[0].path
	later := first[0].signature.ModTime.Add(time.Minute)
	if err := os.Chtimes(touched, later, later); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Remove(first[2].path); err != nil {
		t.Fatalf("remove: %v", err)
	}
	third, err := cache.load(ctx, root)
	if err != nil {
		t.Fatalf("third load: %v", err)
	}
	if len(third) != 2 {
		t.Fatalf("len(third) = %d, want 2", len(third))
	}
	if third[0].path != touched || third[0].session == first[0].session {
		t.Errorf("%s should have been re-read", touched)
	}
	if third[1].session != first[1].session {
		t.Errorf("%s was re-read although unchanged", third[1].path)
	}
	if len(cache.entries) != 2 {
		t.Errorf("cache holds %d entries, want 2", len(cache.entries))
	}
}

func TestSessionCache_IgnoresOtherFiles(t *testing.T) {
	root := t.TempDir()
	writeFixtureSessions(t, root)
	for _, name := range []string{"session.lock", "session.v3.jsonl.zstd", "notes.jsonl"} {
		path := filepath.Join(root, "proj-alpha", "sess-1", name)
		if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	var cache sessionCache
	loaded, err := cache.load(context.Background(), root)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 3 {
		t.Errorf("len(loaded) = %d, want 3", len(loaded))
	}
}
