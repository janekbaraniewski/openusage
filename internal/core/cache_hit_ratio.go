package core

// CacheHitRatio returns the token-weighted prompt cache hit ratio as a
// percentage (0..100) and whether it is defined.
//
// The denominator is the prompt-side token volume: non-cached input tokens +
// cache-read tokens + cache-write (cache-creation) tokens. Output and reasoning
// tokens are excluded because they are never served from cache. ok is false
// when the denominator is zero (no prompt activity, or a provider with no
// caching), so callers can omit the metric rather than render a misleading 0%.
//
// Providers that report cache reads but not cache writes (e.g. gemini_cli)
// simply pass cacheWrite=0; the ratio then reads against input + cache reads.
//
// ok is false when there is no cache activity at all (cacheRead+cacheWrite==0),
// so a model or provider that never touches the cache reports nothing rather
// than a noisy "0% cached". A cold cache (writes but no reads yet) still
// reports a meaningful 0%.
func CacheHitRatio(input, cacheRead, cacheWrite float64) (pct float64, ok bool) {
	if cacheRead+cacheWrite <= 0 {
		return 0, false
	}
	denom := input + cacheRead + cacheWrite
	if denom <= 0 {
		return 0, false
	}
	pct = cacheRead / denom * 100
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return pct, true
}

// CacheHitRatioMetric builds the standard cache_hit_ratio Metric for the given
// window, or returns (zero, false) when the ratio is undefined. The metric is a
// percentage gauge: Used is the hit ratio, Remaining the miss ratio, Limit 100.
func CacheHitRatioMetric(input, cacheRead, cacheWrite float64, window string) (Metric, bool) {
	pct, ok := CacheHitRatio(input, cacheRead, cacheWrite)
	if !ok {
		return Metric{}, false
	}
	remaining := 100 - pct
	limit := 100.0
	return Metric{
		Used:      &pct,
		Remaining: &remaining,
		Limit:     &limit,
		Unit:      "%",
		Window:    window,
	}, true
}
