package core

import "strings"

const QuotaNearLimitPercent = 15.0

// EffectiveStatus returns the status that should be shown for a snapshot.
// When telemetry has quota metrics but no persisted status, infer health
// from the tightest quota bucket.
func EffectiveStatus(snap UsageSnapshot) Status {
	if snap.Status != "" && snap.Status != StatusUnknown {
		return snap.Status
	}
	if status, ok := InferStatusFromQuotaMetrics(snap); ok {
		return status
	}
	return snap.Status
}

func InferStatusFromQuotaMetrics(snap UsageSnapshot) (Status, bool) {
	worst := -1.0
	found := false
	for key, met := range snap.Metrics {
		if !strings.HasPrefix(key, "quota_") {
			continue
		}
		rem := -1.0
		if met.Remaining != nil {
			rem = *met.Remaining
		} else if met.Used != nil {
			rem = 100 - *met.Used
		} else if pct := met.Percent(); pct >= 0 {
			rem = pct
		}
		if rem < 0 {
			continue
		}
		if rem > 100 {
			rem = 100
		}
		if !found || rem < worst {
			worst = rem
			found = true
		}
	}
	if !found {
		return "", false
	}
	if worst <= 0 {
		return StatusLimited, true
	}
	if worst < QuotaNearLimitPercent {
		return StatusNearLimit, true
	}
	return StatusOK, true
}
