package dsh

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

func stubPrice(t *testing.T, provider string) {
	t.Helper()
	orig := priceLookup
	t.Cleanup(func() { priceLookup = orig })
	priceLookup = func(context.Context, string, int) (*pricing.Price, error) {
		return &pricing.Price{
			Provider:                provider,
			InputCostPerMillion:     1,
			OutputCostPerMillion:    2,
			CacheReadCostPerMillion: 0.1,
		}, nil
	}
}

func TestEstimateCostDeepSeekHosted(t *testing.T) {
	stubPrice(t, "deepseek")
	msg := messageEntry{Model: "deepseek-flash", Upstream: deepseekProviderID, Input: 1_000_000, Output: 500_000, CacheRead: 2_000_000, Reasoning: 100_000, Total: 3_500_000}

	cost := estimateCost(msg)

	// 1.00 input + 1.00 output + 0.20 cache read; reasoning is inside output.
	if cost == nil || math.Abs(*cost-2.2) > 1e-9 {
		t.Fatalf("cost = %v, want 2.2", cost)
	}
}

func TestEstimateCostSkipsOtherHostsAndForeignPrices(t *testing.T) {
	stubPrice(t, "deepseek")
	if cost := estimateCost(messageEntry{Model: "deepseek/deepseek-v4.1-flash", Upstream: "command_code", Input: 10, Total: 10}); cost != nil {
		t.Errorf("priced a request hosted by Command Code: %v", *cost)
	}

	stubPrice(t, "prism")
	if cost := estimateCost(messageEntry{Model: "deepseek-flash", Upstream: deepseekProviderID, Input: 10, Total: 10}); cost != nil {
		t.Errorf("used another host's price for a DeepSeek request: %v", *cost)
	}
}

func TestToolLineChanges(t *testing.T) {
	args := func(v map[string]string) json.RawMessage {
		inner, _ := json.Marshal(v)
		outer, _ := json.Marshal(string(inner))
		return outer
	}
	if added, removed := toolLineChanges("edit", args(map[string]string{"old_string": "a\nb\n", "new_string": "a\nb\nc"})); added != 3 || removed != 2 {
		t.Errorf("edit = +%d -%d, want +3 -2", added, removed)
	}
	if added, removed := toolLineChanges("write", args(map[string]string{"content": "one\ntwo\n"})); added != 2 || removed != 0 {
		t.Errorf("write = +%d -%d, want +2 -0", added, removed)
	}
	if added, removed := toolLineChanges("bash", args(map[string]string{"content": "x"})); added != 0 || removed != 0 {
		t.Errorf("bash = +%d -%d, want no line changes", added, removed)
	}
}
