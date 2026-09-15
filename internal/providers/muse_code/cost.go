package muse_code

import (
	"context"

	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// priceLookup resolves a Muse model id to its rates. It is a variable so
// tests can substitute a fixture table without network access.
var priceLookup = func(ctx context.Context, model string) (*pricing.Price, error) {
	return pricing.DefaultResolver().Lookup(ctx, model, 0)
}

// estimateEntryCost prices one step through the shared engine: non-cached
// input at the input rate, cache reads at the cache rate, output plus
// reasoning at the output rate (Meta bills reasoning as output). Models
// with no known price report ok=false so the caller counts their tokens
// but omits their dollars instead of pricing them at $0.
func estimateEntryCost(ctx context.Context, e museModelEntry) (float64, bool) {
	price, err := priceLookup(ctx, e.Model)
	if err != nil || price == nil {
		return 0, false
	}
	cost := pricing.Estimate(price, 0, pricing.Usage{
		InputTokens:      int(e.Input),
		OutputTokens:     int(e.Output + e.Reasoning),
		CacheReadTokens:  int(e.CacheRead),
		CacheWriteTokens: int(e.CacheWrite),
	})
	if cost <= 0 {
		return 0, false
	}
	return cost, true
}
