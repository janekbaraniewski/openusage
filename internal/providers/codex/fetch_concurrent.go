package codex

import (
	"context"
	"strings"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type codexSessionBreakdownResult struct {
	snapshot core.UsageSnapshot
	err      error
}

type codexDailyCreditResult struct {
	snapshot core.UsageSnapshot
	err      error
}

type codexQuotaResult struct {
	snapshot  core.UsageSnapshot
	available bool
	err       error
}

type codexQuotaFetches struct {
	live <-chan codexQuotaResult
	cli  <-chan codexQuotaResult
}

// startCodexQuotaFetches keeps the HTTP and app-server quota sources
// independent. A slow live request must not consume the parent deadline before
// the local app-server fallback gets a chance to answer.
func (p *Provider) startCodexQuotaFetches(
	ctx context.Context,
	acct core.AccountConfig,
	configDir string,
	base core.UsageSnapshot,
) codexQuotaFetches {
	quotaBase := core.NewUsageSnapshot(base.ProviderID, base.AccountID)
	quotaBase.Timestamp = base.Timestamp
	if cliVersion := base.Raw["cli_version"]; cliVersion != "" {
		quotaBase.Raw["cli_version"] = cliVersion
	}

	liveSnap := quotaBase.DeepClone()
	liveDone := make(chan codexQuotaResult, 1)
	go func() {
		available, err := p.fetchLiveUsage(ctx, acct, configDir, &liveSnap)
		liveDone <- codexQuotaResult{snapshot: liveSnap, available: available, err: err}
	}()

	cliSnap := quotaBase.DeepClone()
	cliDone := make(chan codexQuotaResult, 1)
	go func() {
		available, err := p.fetchCLIRateLimits(ctx, acct, configDir, &cliSnap)
		cliDone <- codexQuotaResult{snapshot: cliSnap, available: available, err: err}
	}()

	return codexQuotaFetches{live: liveDone, cli: cliDone}
}

// startCodexSessionBreakdown runs the expensive all-session walk against an
// isolated snapshot. The caller can continue with account/network fetches
// while the local history is being parsed.
func startCodexSessionBreakdown(
	sessionsDir string,
	base core.UsageSnapshot,
	read func(string, *core.UsageSnapshot) error,
) <-chan codexSessionBreakdownResult {
	snapshot := base.DeepClone()
	ensureCodexSnapshotMaps(&snapshot)
	done := make(chan codexSessionBreakdownResult, 1)
	go func() {
		err := read(sessionsDir, &snapshot)
		done <- codexSessionBreakdownResult{snapshot: snapshot, err: err}
	}()
	return done
}

// startCodexDailyCreditFetch keeps the optional daily account request on its
// own snapshot and lifecycle. It must not share the snapshot being populated
// by the main fetch path with the session scanner.
func startCodexDailyCreditFetch(
	ctx context.Context,
	acct core.AccountConfig,
	configDir string,
	base core.UsageSnapshot,
	fetch func(context.Context, core.AccountConfig, string, *core.UsageSnapshot) error,
) <-chan codexDailyCreditResult {
	snapshot := base.DeepClone()
	ensureCodexSnapshotMaps(&snapshot)
	done := make(chan codexDailyCreditResult, 1)
	go func() {
		err := fetch(ctx, acct, configDir, &snapshot)
		done <- codexDailyCreditResult{snapshot: snapshot, err: err}
	}()
	return done
}

func ensureCodexSnapshotMaps(snap *core.UsageSnapshot) {
	if snap == nil {
		return
	}
	snap.EnsureMaps()
	if snap.DailySeries == nil {
		snap.DailySeries = make(map[string][]core.TimePoint)
	}
}

// mergeCodexSessionBreakdown adds local analytics without allowing stale
// session values to overwrite live or CLI quota values already in dst.
func mergeCodexSessionBreakdown(dst, src *core.UsageSnapshot) {
	if dst == nil || src == nil {
		return
	}
	ensureCodexSnapshotMaps(dst)

	for key, metric := range src.Metrics {
		if _, exists := dst.Metrics[key]; !exists {
			dst.Metrics[key] = metric
		}
	}
	for key, reset := range src.Resets {
		if _, exists := dst.Resets[key]; !exists {
			dst.Resets[key] = reset
		}
	}
	for key, value := range src.Attributes {
		if _, exists := dst.Attributes[key]; !exists {
			dst.Attributes[key] = value
		}
	}
	for key, value := range src.Diagnostics {
		if _, exists := dst.Diagnostics[key]; !exists {
			dst.Diagnostics[key] = value
		}
	}
	for key, value := range src.Raw {
		if _, exists := dst.Raw[key]; !exists {
			dst.Raw[key] = value
		}
	}
	for key, points := range src.DailySeries {
		if _, exists := dst.DailySeries[key]; !exists {
			dst.DailySeries[key] = append([]core.TimePoint(nil), points...)
		}
	}
	if len(dst.ModelUsage) == 0 && len(src.ModelUsage) > 0 {
		clone := src.DeepClone()
		dst.ModelUsage = clone.ModelUsage
	}
}

// mergeCodexDailyCreditSnapshot copies only the optional account-credit
// enrichment. The account result is authoritative for this series and may
// replace a same-poll local value.
func mergeCodexDailyCreditSnapshot(dst, src *core.UsageSnapshot) {
	if dst == nil || src == nil {
		return
	}
	ensureCodexSnapshotMaps(dst)
	if points, ok := src.DailySeries[codexCreditUsageDailySeriesKey]; ok {
		dst.DailySeries[codexCreditUsageDailySeriesKey] = append([]core.TimePoint(nil), points...)
	}
	for key, value := range src.Raw {
		if strings.HasPrefix(key, "credit_daily_usage_") {
			dst.Raw[key] = value
		}
	}
	if value, ok := src.Diagnostics["credit_daily_usage"]; ok {
		dst.Diagnostics["credit_daily_usage"] = value
	}
}

// mergeCodexQuotaSnapshot copies only fields produced by a remote quota
// source. Live data wins when present; the CLI result fills gaps such as an
// individual credit limit or a rate-limit window omitted by the HTTP payload.
func mergeCodexQuotaSnapshot(dst, src *core.UsageSnapshot, overwrite bool) {
	if dst == nil || src == nil {
		return
	}
	ensureCodexSnapshotMaps(dst)

	if overwrite && src.Raw["rate_limit_source"] == "live_unavailable" {
		clearRateLimitMetrics(dst)
	}
	for key, metric := range src.Metrics {
		if !isCodexQuotaMetric(key) {
			continue
		}
		if overwrite {
			dst.Metrics[key] = metric
			continue
		}
		if _, exists := dst.Metrics[key]; !exists {
			dst.Metrics[key] = metric
		}
	}
	for key, reset := range src.Resets {
		if !isCodexQuotaMetric(key) {
			continue
		}
		if overwrite {
			dst.Resets[key] = reset
			continue
		}
		if _, exists := dst.Resets[key]; !exists {
			dst.Resets[key] = reset
		}
	}

	for key, value := range src.Raw {
		if !isCodexQuotaRawKey(key) {
			continue
		}
		if overwrite || dst.Raw[key] == "" {
			dst.Raw[key] = value
		}
	}
	if !overwrite && lenCodexRateLimitMetrics(src) > 0 {
		switch dst.Raw["rate_limit_source"] {
		case "", "session", "live_unavailable":
			dst.Raw["rate_limit_source"] = "cli_rpc"
		}
	}
}

func isCodexQuotaMetric(key string) bool {
	return strings.HasPrefix(key, "rate_limit_") || strings.HasPrefix(key, "codex_credit_")
}

func isCodexQuotaRawKey(key string) bool {
	switch key {
	case "account_email", "account_id", "plan_type", "credits", "credit_balance", "quota_api", "rate_limit_source", "rate_limit_warning", "credit_limit_source":
		return true
	default:
		return false
	}
}

func lenCodexRateLimitMetrics(snap *core.UsageSnapshot) int {
	if snap == nil {
		return 0
	}
	count := 0
	for key := range snap.Metrics {
		if strings.HasPrefix(key, "rate_limit_") {
			count++
		}
	}
	return count
}
