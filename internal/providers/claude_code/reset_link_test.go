package claude_code

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBillingBlockResetLinkage(t *testing.T) {
	start := time.Date(2026, 8, 15, 14, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Hour)
	now := start.Add(1 * time.Hour)
	snap := core.NewUsageSnapshot("claude_code", "default")

	applyConversationUsageProjection(&snap, conversationUsageProjection{
		now:               now,
		inCurrentBlock:    true,
		currentBlockStart: start,
		currentBlockEnd:   end,
		blockCostUSD:      1.25,
	})

	if got, ok := snap.Resets["billing_block"]; !ok || !got.Equal(end) {
		t.Errorf("Resets[billing_block] = %v (ok=%v), want %v", got, ok, end)
	}
	if got, ok := snap.Resets["billing_block_start"]; !ok || !got.Equal(start) {
		t.Errorf("Resets[billing_block_start] = %v (ok=%v), want %v", got, ok, start)
	}
	metric, ok := snap.Metrics["5h_block_cost"]
	if !ok {
		t.Fatal("5h_block_cost metric missing")
	}
	if metric.ResetKey != "billing_block" {
		t.Errorf("ResetKey = %q, want %q", metric.ResetKey, "billing_block")
	}
}

func TestUsageMetricResetLinkage(t *testing.T) {
	now := time.Date(2026, 8, 15, 20, 0, 0, 0, time.UTC)
	reset := now.Add(2 * time.Hour)
	snap := core.NewUsageSnapshot("claude_code", "default")
	applyUsageResponse(&usageResponse{
		FiveHour: &usageBucket{
			Utilization: 42,
			ResetsAt:    reset.Format(time.RFC3339),
		},
	}, &snap, now)

	metric := snap.Metrics["usage_five_hour"]
	if metric.ResetKey != "usage_five_hour" {
		t.Errorf("ResetKey = %q, want usage_five_hour", metric.ResetKey)
	}
	if got := snap.Resets[metric.ResetKey]; !got.Equal(reset) {
		t.Errorf("reset = %v, want %v", got, reset)
	}
}
