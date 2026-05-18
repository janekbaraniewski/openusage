package amp

import (
	"path/filepath"
	"testing"
	"time"
)

func TestLoadLedgerRecordsSkipsMalformed(t *testing.T) {
	path := filepath.Join("testdata", "ledger_basic.jsonl")
	ledger, skipped, err := loadLedgerRecords(path)
	if err != nil {
		t.Fatalf("loadLedgerRecords returned error: %v", err)
	}
	// 3 keyed records expected (msg-asst-1, msg-asst-2, msg-orphan-1).
	if len(ledger) != 3 {
		t.Errorf("expected 3 records, got %d", len(ledger))
	}
	// One malformed line + one row with empty to_message_id = 2 skipped.
	if skipped != 2 {
		t.Errorf("expected 2 skipped lines, got %d", skipped)
	}
	if rec, ok := ledger["msg-asst-1"]; !ok || rec.effectiveCost() != 0.025 {
		t.Errorf("missing or wrong credits for msg-asst-1: %+v", rec)
	}
}

func TestLoadLedgerRecordsMissing(t *testing.T) {
	ledger, skipped, err := loadLedgerRecords(filepath.Join("testdata", "no-such-ledger.jsonl"))
	if err != nil {
		t.Fatalf("missing ledger should not error, got %v", err)
	}
	if ledger != nil {
		t.Errorf("expected nil ledger map, got %+v", ledger)
	}
	if skipped != 0 {
		t.Errorf("expected 0 skipped, got %d", skipped)
	}
}

func TestReconcileWithLedgerMergesMatches(t *testing.T) {
	threadTS, _ := time.Parse(time.RFC3339, "2026-05-10T09:00:05Z")
	events := []ampEvent{
		{
			MessageID: "msg-asst-1",
			Model:     "sonnet-4.5",
			Timestamp: threadTS,
			Tokens:    ampTokens{Input: 1200, Output: 350, CacheRead: 800, CacheWrite: 200},
			Source:    "thread",
			ThreadID:  "thread-basic-001",
		},
	}
	ledger := map[string]ampLedgerRecord{
		"msg-asst-1": {
			ToMessageID: "msg-asst-1",
			Model:       "sonnet-4.5",
			Credits:     0.025,
			Timestamp:   "2026-05-10T09:00:06Z",
			Tokens:      &ampTokens{Input: 1100, Output: 400, CacheRead: 850, CacheWrite: 210},
		},
		"msg-orphan": {
			ToMessageID: "msg-orphan",
			Credits:     0.011,
			Timestamp:   "2026-05-12T08:00:00Z",
			Model:       "gpt-4o",
			Tokens:      &ampTokens{Input: 300, Output: 80},
		},
	}
	merged := reconcileWithLedger(events, ledger)
	if len(merged) != 2 {
		t.Fatalf("expected 2 merged events (1 match + 1 orphan), got %d", len(merged))
	}

	// The matched event should be first chronologically (May 10 < May 12).
	match := merged[0]
	if match.Source != "merged" {
		t.Errorf("expected Source 'merged', got %q", match.Source)
	}
	if match.CreditCost != 0.025 {
		t.Errorf("expected CreditCost 0.025, got %v", match.CreditCost)
	}
	// Per-field max: input prefers thread (1200>1100); output prefers ledger (400>350).
	if match.Tokens.Input != 1200 {
		t.Errorf("expected Input max=1200, got %d", match.Tokens.Input)
	}
	if match.Tokens.Output != 400 {
		t.Errorf("expected Output max=400, got %d", match.Tokens.Output)
	}
	if match.Tokens.CacheRead != 850 {
		t.Errorf("expected CacheRead max=850, got %d", match.Tokens.CacheRead)
	}
	if match.Tokens.CacheWrite != 210 {
		t.Errorf("expected CacheWrite max=210, got %d", match.Tokens.CacheWrite)
	}
	// Ledger timestamp wins when present.
	wantTS, _ := time.Parse(time.RFC3339, "2026-05-10T09:00:06Z")
	if !match.Timestamp.Equal(wantTS) {
		t.Errorf("expected ledger timestamp %v, got %v", wantTS, match.Timestamp)
	}

	orphan := merged[1]
	if orphan.Source != "ledger" {
		t.Errorf("expected orphan source 'ledger', got %q", orphan.Source)
	}
	if orphan.MessageID != "msg-orphan" {
		t.Errorf("expected orphan id 'msg-orphan', got %q", orphan.MessageID)
	}
	if orphan.CreditCost != 0.011 {
		t.Errorf("expected orphan cost 0.011, got %v", orphan.CreditCost)
	}
}

func TestReconcileWithLedgerNoMatches(t *testing.T) {
	events := []ampEvent{
		{MessageID: "msg-1", Tokens: ampTokens{Input: 100}, Source: "thread"},
	}
	// Empty ledger; events return as-is, sorted.
	out := reconcileWithLedger(events, nil)
	if len(out) != 1 || out[0].MessageID != "msg-1" {
		t.Errorf("unexpected output: %+v", out)
	}

	// Ledger with no matching key — event passes through, ledger row appended.
	ledger := map[string]ampLedgerRecord{
		"other": {ToMessageID: "other", Credits: 0.5},
	}
	out = reconcileWithLedger(events, ledger)
	if len(out) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out))
	}
	// Original event passes through with Source 'thread'; the ledger-only one is 'ledger'.
	var sawThread, sawLedger bool
	for _, evt := range out {
		switch evt.Source {
		case "thread":
			sawThread = true
		case "ledger":
			sawLedger = true
		}
	}
	if !sawThread || !sawLedger {
		t.Errorf("missing expected sources: thread=%v ledger=%v", sawThread, sawLedger)
	}
}

func TestMaxMergeLedgerRecord(t *testing.T) {
	a := ampLedgerRecord{ToMessageID: "id", Credits: 0.5, Model: "m1"}
	b := ampLedgerRecord{ToMessageID: "id", Credits: 0.8, Tokens: &ampTokens{Input: 10}}
	merged := maxMergeLedgerRecord(a, b)
	if merged.effectiveCost() != 0.8 {
		t.Errorf("expected merged cost 0.8, got %v", merged.effectiveCost())
	}
	if merged.Model != "m1" {
		t.Errorf("expected model preserved, got %q", merged.Model)
	}
	if merged.effectiveTokens().Input != 10 {
		t.Errorf("expected tokens.Input=10, got %d", merged.effectiveTokens().Input)
	}
}
