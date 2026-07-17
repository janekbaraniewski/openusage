package codex

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindLastTokenCountDoesNotScanLargeHistoricalPrefix(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rollout-large-prefix.jsonl")
	largeHistoricalLine := `{"type":"response_item","payload":{"type":"function_call_output","output":"` + strings.Repeat("x", maxScannerBufferSize) + `"}}` + "\n"
	latest := `{"timestamp":"2026-07-17T10:00:02Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":15,"output_tokens":10,"total_tokens":25}}}}` + "\n"
	if err := os.WriteFile(path, []byte(largeHistoricalLine+latest), 0o644); err != nil {
		t.Fatalf("write session: %v", err)
	}

	payload, err := findLastTokenCount(path)
	if err != nil {
		t.Fatalf("findLastTokenCount() error: %v", err)
	}
	if payload == nil || payload.Info == nil {
		t.Fatal("findLastTokenCount() returned no token payload")
	}
	if got := payload.Info.TotalTokenUsage.TotalTokens; got != 25 {
		t.Fatalf("total_tokens = %d, want 25", got)
	}
}

func TestSessionUsageBreakdownsCanBeDisabled(t *testing.T) {
	t.Setenv("OPENUSAGE_CODEX_SKIP_SESSION_BREAKDOWNS", "true")
	if codexSessionUsageBreakdownsEnabled() {
		t.Fatal("session usage breakdowns enabled with skip flag set")
	}

	t.Setenv("OPENUSAGE_CODEX_SKIP_SESSION_BREAKDOWNS", "false")
	if !codexSessionUsageBreakdownsEnabled() {
		t.Fatal("session usage breakdowns disabled without skip flag")
	}
}
