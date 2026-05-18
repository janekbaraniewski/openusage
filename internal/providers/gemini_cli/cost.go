package gemini_cli

import (
	"context"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// priceLookupTimeout bounds the dynamic pricing query so a slow upstream
// cannot stall a Fetch.
const priceLookupTimeout = 2 * time.Second

// priceLookup is the indirection used by estimateUsageCost to query the
// dynamic pricing package. Tests override this to inject fixtures.
var priceLookup = func(ctx context.Context, model string, ctxLen int) (*pricing.Price, error) {
	return pricing.DefaultResolver().Lookup(ctx, model, ctxLen)
}

// estimateUsageCost returns the USD cost of a single delta usage record at
// the resolved model rate. When the resolver fails (offline / unknown
// model) the function returns 0 so callers can keep aggregating without a
// nil guard.
func estimateUsageCost(model string, delta tokenUsage) float64 {
	if delta.TotalTokens <= 0 {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), priceLookupTimeout)
	defer cancel()
	p, err := priceLookup(ctx, model, 0)
	if err != nil || p == nil {
		return 0
	}
	return pricing.Estimate(p, 0, pricing.Usage{
		InputTokens:     delta.InputTokens,
		OutputTokens:    delta.OutputTokens,
		CacheReadTokens: delta.CachedInputTokens,
		ReasoningTokens: delta.ReasoningTokens,
	})
}

// emitCostMetrics publishes per-model and aggregate cost metrics onto the
// snapshot when at least one model resolved a non-zero rate.
func emitCostMetrics(modelCost, dailyCost map[string]float64, totalCostUSD, todayCostUSD float64, snap *core.UsageSnapshot) {
	if totalCostUSD <= 0 && todayCostUSD <= 0 && len(modelCost) == 0 {
		return
	}
	for name, cost := range modelCost {
		if cost <= 0 {
			continue
		}
		v := cost
		snap.Metrics["model_"+sanitizeMetricName(name)+"_cost_usd"] = core.Metric{
			Used:   &v,
			Unit:   "USD",
			Window: defaultUsageWindowLabel,
		}
	}
	if totalCostUSD > 0 {
		v := totalCostUSD
		snap.Metrics["total_cost_usd"] = core.Metric{Used: &v, Unit: "USD", Window: defaultUsageWindowLabel}
	}
	if todayCostUSD > 0 {
		v := todayCostUSD
		snap.Metrics["today_cost"] = core.Metric{Used: &v, Unit: "USD", Window: "today"}
	}
	if len(dailyCost) > 0 {
		snap.DailySeries["cost_usd"] = core.SortedTimePoints(dailyCost)
	}
}
