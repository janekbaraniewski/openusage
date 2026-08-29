package codex

import (
	"context"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestCodexDailyCreditTaskRunsWhileSessionBreakdownIsBlocked(t *testing.T) {
	base := core.NewUsageSnapshot("codex", "account")
	base.DailySeries = make(map[string][]core.TimePoint)

	scanStarted := make(chan struct{})
	releaseScan := make(chan struct{})
	scanDone := startCodexSessionBreakdown("/sessions", base, func(_ string, _ *core.UsageSnapshot) error {
		close(scanStarted)
		<-releaseScan
		return nil
	})
	<-scanStarted

	dailyFinished := make(chan struct{})
	dailyDone := startCodexDailyCreditFetch(
		context.Background(),
		core.AccountConfig{ID: "account"},
		"/config",
		base,
		func(_ context.Context, _ core.AccountConfig, _ string, _ *core.UsageSnapshot) error {
			close(dailyFinished)
			return nil
		},
	)

	select {
	case <-dailyFinished:
	case <-time.After(time.Second):
		t.Fatal("daily credit fetch waited for the blocked session breakdown")
	}

	close(releaseScan)
	if result := <-scanDone; result.err != nil {
		t.Fatalf("session breakdown returned error: %v", result.err)
	}
	if result := <-dailyDone; result.err != nil {
		t.Fatalf("daily credit fetch returned error: %v", result.err)
	}
}

func TestMergeCodexSessionBreakdownPreservesRemoteQuota(t *testing.T) {
	remote := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&remote)
	remoteUsed := 90.0
	remoteLimit := 100.0
	remoteReset := time.Date(2026, 8, 21, 0, 0, 0, 0, time.UTC)
	remote.Metrics["codex_credit_limit"] = core.Metric{Limit: &remoteLimit, Used: &remoteUsed, Unit: "credits"}
	remote.Resets["codex_credit_limit"] = remoteReset
	remote.Raw["rate_limit_source"] = "live"
	remote.DailySeries[codexCreditUsageDailySeriesKey] = []core.TimePoint{{Date: "2026-08-20", Value: 90}}

	local := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&local)
	localUsed := 10.0
	localLimit := 100.0
	localReset := remoteReset.Add(-24 * time.Hour)
	local.Metrics["codex_credit_limit"] = core.Metric{Limit: &localLimit, Used: &localUsed, Unit: "credits"}
	local.Resets["codex_credit_limit"] = localReset
	local.Metrics["model_gpt_5_codex_total_tokens"] = core.Metric{Used: core.Float64Ptr(42), Unit: "tokens"}
	local.Raw["model_usage"] = "gpt-5-codex 42"
	local.DailySeries["tokens_model_gpt_5_codex"] = []core.TimePoint{{Date: "2026-08-20", Value: 42}}

	mergeCodexSessionBreakdown(&remote, &local)

	if got := *remote.Metrics["codex_credit_limit"].Used; got != remoteUsed {
		t.Fatalf("remote credit usage was overwritten: got %.1f, want %.1f", got, remoteUsed)
	}
	if got := remote.Resets["codex_credit_limit"]; !got.Equal(remoteReset) {
		t.Fatalf("remote credit reset was overwritten: got %s, want %s", got, remoteReset)
	}
	if got := remote.Raw["rate_limit_source"]; got != "live" {
		t.Fatalf("remote rate-limit source was overwritten: got %q", got)
	}
	if _, ok := remote.Metrics["model_gpt_5_codex_total_tokens"]; !ok {
		t.Fatal("local breakdown metric was not merged")
	}
	if got := remote.Raw["model_usage"]; got != "gpt-5-codex 42" {
		t.Fatalf("local breakdown raw summary was not merged: %q", got)
	}
	if got := remote.DailySeries[codexCreditUsageDailySeriesKey][0].Value; got != 90 {
		t.Fatalf("account daily series was overwritten: got %.1f, want 90", got)
	}
}

func TestMergeCodexQuotaSnapshotUsesCLIFallbackWhenLiveIsUnavailable(t *testing.T) {
	dst := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&dst)
	used := 80.0
	limit := 100.0
	dst.Metrics["rate_limit_primary"] = core.Metric{Limit: &limit, Used: &used, Unit: "%", Window: "5h"}
	dst.Raw["rate_limit_source"] = "session"

	live := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&live)
	live.Raw["rate_limit_source"] = "live_unavailable"
	mergeCodexQuotaSnapshot(&dst, &live, true)
	if _, ok := dst.Metrics["rate_limit_primary"]; ok {
		t.Fatal("live-unavailable result should clear stale session rate limits")
	}

	cli := core.NewUsageSnapshot("codex", "account")
	ensureCodexSnapshotMaps(&cli)
	cliUsed := 20.0
	cliLimit := 100.0
	cli.Metrics["rate_limit_primary"] = core.Metric{Limit: &cliLimit, Used: &cliUsed, Unit: "%", Window: "5h"}
	cli.Raw["rate_limit_source"] = "cli_rpc"
	mergeCodexQuotaSnapshot(&dst, &cli, false)

	if got := *dst.Metrics["rate_limit_primary"].Used; got != 20 {
		t.Fatalf("CLI fallback used = %.1f, want 20", got)
	}
	if got := dst.Raw["rate_limit_source"]; got != "cli_rpc" {
		t.Fatalf("rate_limit_source = %q, want cli_rpc", got)
	}
}
