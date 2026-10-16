package dsh

import (
	"context"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/pricing"
)

// priceLookupTimeout bounds the pricing query so a slow lookup cannot stall
// a collection pass.
const priceLookupTimeout = 2 * time.Second

// priceLookup is the indirection estimateCost uses to query the pricing
// package. Tests override it to inject fixtures.
var priceLookup = func(ctx context.Context, model string, ctxLen int) (*pricing.Price, error) {
	return pricing.DefaultResolver().Lookup(ctx, model, ctxLen)
}

// estimateCost prices a message at the published per-token rate of the
// service that hosted it. It covers only requests sent straight to the
// DeepSeek platform, which bills by token at that rate. The other hosts dsh
// uses bill by subscription or by their own credits, so a list price would
// not be what the request cost. It returns nil when no price applies.
func estimateCost(msg messageEntry) *float64 {
	if msg.Upstream != deepseekProviderID || msg.Total <= 0 {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), priceLookupTimeout)
	defer cancel()

	ctxLen := int(msg.Input + msg.CacheRead)
	price, err := priceLookup(ctx, msg.Model, ctxLen)
	if err != nil || price == nil || !strings.EqualFold(price.Provider, deepseekProviderID) {
		return nil
	}
	// Reasoning tokens are part of the output count, so they are not
	// priced a second time.
	cost := pricing.Estimate(price, ctxLen, pricing.Usage{
		InputTokens:      int(msg.Input),
		OutputTokens:     int(msg.Output),
		CacheReadTokens:  int(msg.CacheRead),
		CacheWriteTokens: int(msg.CacheWrite),
	})
	if cost <= 0 {
		return nil
	}
	return core.Float64Ptr(cost)
}
