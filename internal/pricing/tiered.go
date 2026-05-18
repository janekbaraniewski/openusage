package pricing

import "sort"

// canonical tier thresholds (in tokens) supported by the resolver.
var tierThresholds = []int{128_000, 200_000, 256_000, 272_000}

// ResolveTier walks a tier ladder (sorted ascending by AppliesAbove) and
// returns the most specific Price whose threshold the supplied contextLen
// crosses. If no entry matches, it returns the lowest tier (base) or a
// zero Price when prices is empty.
//
// The function does not mutate prices.
func ResolveTier(prices []TieredPrice, contextLen int) Price {
	if len(prices) == 0 {
		return Price{}
	}
	sorted := make([]TieredPrice, len(prices))
	copy(sorted, prices)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].AppliesAbove < sorted[j].AppliesAbove
	})

	chosen := sorted[0].Price
	for _, tp := range sorted {
		if contextLen > tp.AppliesAbove {
			chosen = tp.Price
			continue
		}
		break
	}
	return chosen
}

// ApplyTier resolves base + tier overrides at a given context length and
// returns a flat Price whose rates already reflect the chosen tier (so
// callers don't have to inspect Tiers themselves).
//
// Tier overrides are layered: as contextLen crosses each threshold
// (128k, 200k, 256k, 272k), any non-nil fields in that tier override the
// running value. This way a tier that only sets, say, InputCostPerMillion
// does not clobber an OutputCostPerMillion set by a lower tier.
//
// Pass contextLen == 0 to get the base rates untouched.
func ApplyTier(base Price, contextLen int) Price {
	out := base
	out.Tiers = TierOverrides{}
	type cand struct {
		threshold int
		rates     *TierRates
	}
	cands := []cand{
		{128_000, base.Tiers.Above128k},
		{200_000, base.Tiers.Above200k},
		{256_000, base.Tiers.Above256k},
		{272_000, base.Tiers.Above272k},
	}
	for _, c := range cands {
		if c.rates == nil {
			continue
		}
		if contextLen <= c.threshold {
			continue
		}
		if c.rates.InputCostPerMillion != nil {
			out.InputCostPerMillion = *c.rates.InputCostPerMillion
		}
		if c.rates.OutputCostPerMillion != nil {
			out.OutputCostPerMillion = *c.rates.OutputCostPerMillion
		}
		if c.rates.CacheReadCostPerMillion != nil {
			out.CacheReadCostPerMillion = *c.rates.CacheReadCostPerMillion
		}
		if c.rates.CacheWriteCostPerMillion != nil {
			out.CacheWriteCostPerMillion = *c.rates.CacheWriteCostPerMillion
		}
	}
	return out
}
