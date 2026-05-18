package gemini_cli

import (
	"context"
	"math"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

func TestEstimateUsageCost_UsesResolver(t *testing.T) {
	prev := priceLookup
	priceLookup = func(_ context.Context, _ string, _ int) (*pricing.Price, error) {
		return &pricing.Price{
			ModelID:              "stub",
			Source:               pricing.SourceHardcoded,
			InputCostPerMillion:  1.25,
			OutputCostPerMillion: 5.0,
		}, nil
	}
	t.Cleanup(func() { priceLookup = prev })

	// 1M input @ $1.25 + 200k output @ $5 = 1.25 + 1.0 = 2.25
	delta := tokenUsage{
		InputTokens:  1_000_000,
		OutputTokens: 200_000,
		TotalTokens:  1_200_000,
	}
	got := estimateUsageCost("gemini-1.5-pro", delta)
	want := 2.25
	if math.Abs(got-want) > 0.001 {
		t.Errorf("estimateUsageCost = %.4f, want %.4f", got, want)
	}
}

func TestEstimateUsageCost_ResolverErrorReturnsZero(t *testing.T) {
	// TestMain installs an erroring stub already, so this just confirms the
	// no-price path returns 0 instead of crashing.
	delta := tokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150}
	if got := estimateUsageCost("anything", delta); got != 0 {
		t.Errorf("estimateUsageCost on resolver error = %f, want 0", got)
	}
}

func TestEstimateUsageCost_ZeroDeltaReturnsZero(t *testing.T) {
	if got := estimateUsageCost("anything", tokenUsage{}); got != 0 {
		t.Errorf("estimateUsageCost on zero delta = %f, want 0", got)
	}
}
