package telemetry

import (
	"testing"
	"time"
)

func TestBuildDedupKey_StableIDPriorityUsesToolCallID(t *testing.T) {
	now := time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC)

	one := IngestRequest{
		SourceSystem: SourceSystem("codex"),
		OccurredAt:   now,
		SessionID:    "session-1",
		TurnID:       "turn-1",
		MessageID:    "message-A",
		ToolCallID:   "tool-1",
		EventType:    EventTypeToolUsage,
	}
	two := one
	two.MessageID = "message-B"
	two.TurnID = "turn-2"

	if BuildDedupKey(one) != BuildDedupKey(two) {
		t.Fatal("expected same dedup key when tool_call_id is unchanged")
	}
}

func TestBuildDedupKey_StableIDIgnoresTimestampAndMetrics(t *testing.T) {
	inputA := int64(10)
	inputB := int64(900)

	one := IngestRequest{
		SourceSystem: SourceSystem("codex"),
		OccurredAt:   time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		SessionID:    "session-1",
		MessageID:    "msg-1",
		EventType:    EventTypeMessageUsage,
		InputTokens:  &inputA,
	}
	two := one
	two.OccurredAt = time.Date(2026, time.February, 22, 13, 5, 0, 0, time.UTC)
	two.InputTokens = &inputB

	if BuildDedupKey(one) != BuildDedupKey(two) {
		t.Fatal("expected stable IDs to dominate dedup key despite timestamp/metric drift")
	}
}

func TestBuildDedupKey_StableIDIgnoresModelDrift(t *testing.T) {
	one := IngestRequest{
		SourceSystem: SourceSystem("opencode"),
		OccurredAt:   time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		SessionID:    "session-1",
		MessageID:    "msg-1",
		EventType:    EventTypeMessageUsage,
		ModelRaw:     "qwen/qwen3-coder-flash",
	}
	two := one
	two.ModelRaw = "anthropic/claude-sonnet-4.5"

	if BuildDedupKey(one) != BuildDedupKey(two) {
		t.Fatal("expected stable IDs to dominate dedup key despite model drift")
	}
}

func TestBuildDedupKey_StableIDIgnoresProviderAccountAgentDrift(t *testing.T) {
	one := IngestRequest{
		SourceSystem: SourceSystem("opencode"),
		OccurredAt:   time.Date(2026, time.February, 22, 13, 0, 0, 0, time.UTC),
		SessionID:    "session-1",
		MessageID:    "msg-1",
		EventType:    EventTypeMessageUsage,
		ProviderID:   "openrouter",
		AccountID:    "zen",
		AgentName:    "build",
	}
	two := one
	two.ProviderID = "anthropic"
	two.AccountID = "openrouter"
	two.AgentName = "opencode"

	if BuildDedupKey(one) != BuildDedupKey(two) {
		t.Fatal("expected stable IDs to dominate dedup key despite provider/account/agent drift")
	}
}

func TestBuildDedupKey_StableIDTrimsWhitespace(t *testing.T) {
	one := IngestRequest{
		SourceSystem: SourceSystem("opencode"),
		SessionID:    "sess-1",
		MessageID:    "msg-1",
		EventType:    EventTypeMessageUsage,
	}
	two := IngestRequest{
		SourceSystem: SourceSystem(" opencode "),
		SessionID:    " sess-1 ",
		MessageID:    " msg-1 ",
		EventType:    EventType(" message_usage "),
	}

	if BuildDedupKey(one) != BuildDedupKey(two) {
		t.Fatal("expected dedup key to ignore whitespace drift")
	}
}

func TestBuildDedupKey_FallbackFingerprintIncludesTokenTuple(t *testing.T) {
	now := time.Date(2026, time.February, 22, 13, 1, 0, 0, time.UTC)
	inputA := int64(100)
	inputB := int64(101)

	a := IngestRequest{
		SourceSystem: SourceSystem("opencode"),
		OccurredAt:   now,
		EventType:    EventTypeMessageUsage,
		InputTokens:  &inputA,
	}
	b := a
	b.InputTokens = &inputB

	if BuildDedupKey(a) == BuildDedupKey(b) {
		t.Fatal("expected different dedup keys when token tuple differs")
	}
}

func TestNormalizeRequest_InferTotalTokens(t *testing.T) {
	in := int64(4)
	out := int64(6)
	norm := normalizeRequest(IngestRequest{
		SourceSystem: SourceSystem("claude_code"),
		InputTokens:  &in,
		OutputTokens: &out,
	}, time.Date(2026, time.February, 22, 13, 2, 0, 0, time.UTC))

	if norm.TotalTokens == nil {
		t.Fatal("expected total tokens to be inferred")
	}
	if *norm.TotalTokens != 10 {
		t.Fatalf("total tokens = %d, want 10", *norm.TotalTokens)
	}
}
