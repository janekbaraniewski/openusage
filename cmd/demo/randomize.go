package main

import (
	"math"
	"math/rand"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func randomizeDemoSnapshots(snaps map[string]core.UsageSnapshot, now time.Time, rng *rand.Rand) {
	for accountID, snap := range snaps {
		for key, metric := range snap.Metrics {
			snap.Metrics[key] = randomizeDemoMetric(key, metric, rng)
		}

		for key, resetAt := range snap.Resets {
			resetIn := resetAt.Sub(now)
			if resetIn <= 0 {
				continue
			}
			snap.Resets[key] = now.Add(jitterDuration(resetIn, 0.25, rng))
		}

		snap.Message = demoMessageForSnapshot(snap)
		snaps[accountID] = snap
	}
}

func randomizeDemoMetric(key string, metric core.Metric, rng *rand.Rand) core.Metric {
	hasLimit := metric.Limit != nil && *metric.Limit > 0
	hasRemaining := metric.Remaining != nil
	hasUsed := metric.Used != nil

	if hasLimit && (hasRemaining || hasUsed) {
		limit := *metric.Limit
		used := limit * (0.12 + rng.Float64()*0.8)
		if used < 0 {
			used = 0
		}
		if used > limit {
			used = limit
		}

		if hasUsed {
			updatedUsed := roundLike(*metric.Used, used)
			metric.Used = ptr(updatedUsed)
		}
		if hasRemaining {
			remaining := limit - used
			if remaining < 0 {
				remaining = 0
			}
			updatedRemaining := roundLike(*metric.Remaining, remaining)
			metric.Remaining = ptr(updatedRemaining)
		}
		return metric
	}

	if hasUsed {
		used := syntheticMetricValue(key, metric.Unit, rng)
		if used < 0 {
			used = 0
		}
		metric.Used = ptr(roundLike(*metric.Used, used))
	}
	if hasRemaining {
		remaining := syntheticMetricValue(key, metric.Unit, rng)
		if remaining < 0 {
			remaining = 0
		}
		metric.Remaining = ptr(roundLike(*metric.Remaining, remaining))
	}

	return metric
}

func syntheticMetricValue(key, unit string, rng *rand.Rand) float64 {
	lkey := strings.ToLower(key)
	lunit := strings.ToLower(unit)

	switch {
	case lunit == "flag":
		if rng.Float64() < 0.82 {
			return 0
		}
		return 1
	case strings.Contains(lkey, "price_") || strings.Contains(lunit, "/1mtok"):
		return 0.01 + rng.Float64()*25
	case strings.Contains(lkey, "cost") || strings.Contains(lunit, "usd") || strings.Contains(lunit, "eur") || strings.Contains(lunit, "cny"):
		return 0.1 + rng.Float64()*700
	case strings.Contains(lunit, "token") || strings.Contains(lunit, "char"):
		return 1000 + rng.Float64()*9_000_000
	case strings.Contains(lunit, "bytes"):
		return 5_000_000 + rng.Float64()*120_000_000_000
	case strings.Contains(lunit, "request") || strings.Contains(lunit, "message") || strings.Contains(lunit, "session") || strings.Contains(lunit, "call") || strings.Contains(lunit, "turn"):
		return 1 + rng.Float64()*6000
	case strings.Contains(lunit, "models"):
		return 1 + rng.Float64()*120
	case strings.Contains(lunit, "seats"):
		return 1 + rng.Float64()*80
	case strings.Contains(lunit, "ms"):
		return 20 + rng.Float64()*950
	case lunit == "%":
		return 1 + rng.Float64()*98
	case strings.Contains(lunit, "lines"):
		return 1 + rng.Float64()*500
	case strings.Contains(lunit, "days"):
		return 1 + rng.Float64()*31
	default:
		return 1 + rng.Float64()*5000
	}
}

func jitterDuration(base time.Duration, maxDelta float64, rng *rand.Rand) time.Duration {
	if base <= 0 {
		return base
	}
	factor := 1 + ((rng.Float64()*2 - 1) * maxDelta)
	jittered := time.Duration(float64(base) * factor)
	if jittered < 5*time.Second {
		return 5 * time.Second
	}
	return jittered
}

func roundLike(original, value float64) float64 {
	if math.Abs(original-math.Round(original)) < 1e-9 {
		return math.Round(value)
	}
	return math.Round(value*100) / 100
}
