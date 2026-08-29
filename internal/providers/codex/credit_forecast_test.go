package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyCreditLimitDetails(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "test")
	resetAt := float64(1785542400)
	details := &creditLimitDetails{
		Limit:            "7500",
		Used:             "2572.3221212625504",
		RemainingPercent: float64(66),
		ResetsAt:         resetAt,
	}

	if !applyCreditLimitDetails(details, &snap, "cli") {
		t.Fatal("expected credit limit to be applied")
	}

	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok {
		t.Fatal("expected codex_credit_limit metric")
	}
	if metric.Limit == nil || *metric.Limit != 7500 {
		t.Fatalf("expected credit limit 7500, got %v", metric.Limit)
	}
	if metric.Used == nil || *metric.Used < 2572.32 || *metric.Used > 2572.33 {
		t.Fatalf("expected used credits around 2572.32, got %v", metric.Used)
	}
	if metric.Remaining == nil || *metric.Remaining < 4927.67 || *metric.Remaining > 4927.68 {
		t.Fatalf("expected remaining credits around 4927.68, got %v", metric.Remaining)
	}

	percent, ok := snap.Metrics["codex_credit_percent_used"]
	if !ok || percent.Used == nil || *percent.Used < 34.29 || *percent.Used > 34.30 {
		t.Fatalf("expected used percentage around 34.30, got %+v", percent)
	}
	if got := snap.Resets["codex_credit_limit"].Unix(); got != int64(resetAt) {
		t.Fatalf("expected reset %d, got %d", int64(resetAt), got)
	}
	if snap.Raw["credit_limit_source"] != "cli" {
		t.Fatalf("expected cli source, got %q", snap.Raw["credit_limit_source"])
	}
}

func TestApplyCodexCLIRateLimits(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "test")
	resultJSON := []byte(`{
		"rateLimits": {
			"credits": {"hasCredits": true, "unlimited": false, "balance": null},
			"individualLimit": {"limit": "7500", "used": "2572.3221212625504", "remainingPercent": 66, "resetsAt": 1785542400},
			"planType": "business"
		},
		"rateLimitsByLimitId": {}
	}`)
	var result codexCLIRateLimitsResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatal(err)
	}

	if !applyCodexCLIRateLimits(result, &snap) {
		t.Fatal("expected CLI rate limits to apply")
	}
	if snap.Raw["plan_type"] != "business" {
		t.Fatalf("expected business plan, got %q", snap.Raw["plan_type"])
	}
	if snap.Raw["credits"] != "available" {
		t.Fatalf("expected available credits, got %q", snap.Raw["credits"])
	}
	if snap.Raw["quota_api"] != "cli_rpc" {
		t.Fatalf("expected cli_rpc quota source, got %q", snap.Raw["quota_api"])
	}
}

func TestApplyCodexCLIRateLimitsParsesCurrentAppServerWindows(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "test")
	resultJSON := []byte(`{
		"rateLimits": {
			"primary": {"usedPercent": 20, "windowDurationMins": 300, "resetsAt": 1787970883},
			"secondary": {"usedPercent": 47, "windowDurationMins": 10080, "resetsAt": 1788455611},
			"credits": {"hasCredits": false, "unlimited": false, "balance": "0"},
			"planType": "plus"
		},
		"rateLimitsByLimitId": {
			"codex": {
				"primary": {"usedPercent": 20, "windowDurationMins": 300, "resetsAt": 1787970883},
				"secondary": {"usedPercent": 47, "windowDurationMins": 10080, "resetsAt": 1788455611}
			}
		}
	}`)
	var result codexCLIRateLimitsResult
	if err := json.Unmarshal(resultJSON, &result); err != nil {
		t.Fatal(err)
	}

	if !applyCodexCLIRateLimits(result, &snap) {
		t.Fatal("expected current app-server rate limits to apply")
	}
	primary := snap.Metrics["rate_limit_primary"]
	if primary.Used == nil || *primary.Used != 20 {
		got := float64(0)
		if primary.Used != nil {
			got = *primary.Used
		}
		t.Fatalf("rate_limit_primary used = %.1f, want 20", got)
	}
	secondary := snap.Metrics["rate_limit_secondary"]
	if secondary.Used == nil || *secondary.Used != 47 {
		got := float64(0)
		if secondary.Used != nil {
			got = *secondary.Used
		}
		t.Fatalf("rate_limit_secondary used = %.1f, want 47", got)
	}
	if got := snap.Metrics["rate_limit_primary"].Window; got != "5h" {
		t.Fatalf("rate_limit_primary window = %q, want 5h", got)
	}
	if got := snap.Metrics["rate_limit_secondary"].Window; got != "7d" {
		t.Fatalf("rate_limit_secondary window = %q, want 7d", got)
	}
	if got := snap.Resets["rate_limit_primary"].Unix(); got != 1787970883 {
		t.Fatalf("rate_limit_primary reset = %d, want 1787970883", got)
	}
	if got := snap.Raw["plan_type"]; got != "plus" {
		t.Fatalf("plan_type = %q, want plus", got)
	}
	if got := snap.Raw["rate_limit_source"]; got != "cli_rpc" {
		t.Fatalf("rate_limit_source = %q, want cli_rpc", got)
	}
}

func TestFetchUsesCLIRateLimits(t *testing.T) {
	tmpDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpDir, "auth.json"), []byte(`{"tokens":{}}`), 0600); err != nil {
		t.Fatal(err)
	}

	previous := fetchCodexRateLimitsRPC
	defer func() { fetchCodexRateLimitsRPC = previous }()
	fetchCodexRateLimitsRPC = func(context.Context, core.AccountConfig, string) (codexCLIRateLimitsResult, error) {
		return codexCLIRateLimitsResult{
			RateLimitsV2: &codexCLIRateLimitsSnapshot{
				IndividualLimitV2: &creditLimitDetails{Limit: "7500", Used: "2500"},
				PlanTypeV2:        "business",
			},
		}, nil
	}

	p := New()
	snap, err := p.Fetch(context.Background(), core.AccountConfig{
		ID:       "codex-test",
		Provider: "codex",
		RuntimeHints: map[string]string{
			"config_dir": tmpDir,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	metric, ok := snap.Metrics["codex_credit_limit"]
	if !ok || metric.Used == nil || *metric.Used != 2500 {
		t.Fatalf("expected CLI credit metric, got %+v", metric)
	}
	if snap.Raw["credit_limit_source"] != "cli" {
		t.Fatalf("expected CLI credit source, got %q", snap.Raw["credit_limit_source"])
	}
}

func TestApplyCreditForecast(t *testing.T) {
	p := New()
	start := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)

	first := core.NewUsageSnapshot("codex", "test")
	first.Timestamp = start
	applyCreditLimitDetails(&creditLimitDetails{Limit: "1000", Used: "100"}, &first, "cli")
	p.applyCreditForecast(&first, "test")

	second := core.NewUsageSnapshot("codex", "test")
	second.Timestamp = start.Add(time.Hour)
	applyCreditLimitDetails(&creditLimitDetails{Limit: "1000", Used: "300"}, &second, "cli")
	p.applyCreditForecast(&second, "test")

	rate := second.Metrics["codex_credit_burn_rate"]
	if rate.Used == nil || *rate.Used < 199.99 || *rate.Used > 200.01 {
		t.Fatalf("expected 200 credits/hour, got %v", rate.Used)
	}
	if rate.Window != "observed" {
		t.Fatalf("expected observed forecast window, got %q", rate.Window)
	}
	runout := second.Metrics["codex_credit_runout_hours"]
	if runout.Used == nil || *runout.Used < 3.49 || *runout.Used > 3.51 {
		t.Fatalf("expected 3.5 hours to run out, got %v", runout.Used)
	}
}

func TestApplyCreditForecastUsesInferredMonthlyStart(t *testing.T) {
	p := New()
	observedAt := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	applyCreditLimitDetails(&creditLimitDetails{
		Limit:    "7500",
		Used:     "1450",
		ResetsAt: float64(resetAt.Unix()),
	}, &snap, "cli")
	p.applyCreditForecast(&snap, "test")

	rate := snap.Metrics["codex_credit_burn_rate"]
	// July 1 00:00 -> July 15 12:00 is 348 hours; 1450 / 348 ≈ 4.1667.
	if rate.Used == nil {
		t.Fatal("expected inferred-period burn rate")
	}
	if *rate.Used < 4.16 || *rate.Used > 4.17 {
		t.Fatalf("expected inferred-period rate around 4.1667 credits/hour, got %.4f (period start %q)", *rate.Used, snap.Raw["credit_forecast_period_start"])
	}
	if rate.Window != "current-period average" {
		t.Fatalf("expected current-period average window, got %q", rate.Window)
	}
	runout := snap.Metrics["codex_credit_runout_hours"]
	if runout.Used == nil {
		t.Fatal("expected inferred-period runout")
	}
	if *runout.Used < 1451 || *runout.Used > 1453 {
		t.Fatalf("expected about 1452 hours to run out, got %.2f", *runout.Used)
	}
	if snap.Raw["credit_forecast_source"] != "inferred_period_start" {
		t.Fatalf("expected inferred forecast source, got %q", snap.Raw["credit_forecast_source"])
	}
	if got := snap.Raw["credit_forecast_period_start"]; got != "2026-07-01T00:00:00Z" {
		t.Fatalf("expected inferred period start, got %q", got)
	}
}

func TestApplyCreditForecastProjectsZeroUsageReserveWithoutDailyHistory(t *testing.T) {
	observedAt := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	resetAt := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)

	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	applyCreditLimitDetails(&creditLimitDetails{
		Limit:    "7500",
		Used:     "0",
		ResetsAt: float64(resetAt.Unix()),
	}, &snap, "cli")
	New().applyCreditForecast(&snap, "test")

	projected := snap.Metrics["codex_credit_projected_credits_at_reset"]
	if projected.Used == nil || *projected.Used != 0 {
		t.Fatalf("expected zero projected credits, got %v", projected.Used)
	}
	reserve := snap.Metrics["codex_credit_projected_reserve_at_reset"]
	if reserve.Used == nil || *reserve.Used != 7500 {
		t.Fatalf("expected full projected reserve, got %v", reserve.Used)
	}
	if snap.Raw["credit_forecast_source"] != "inferred_period_start" {
		t.Fatalf("expected inferred forecast source, got %q", snap.Raw["credit_forecast_source"])
	}
}

func TestApplyCreditForecastResetsAfterQuotaReset(t *testing.T) {
	p := New()
	start := time.Date(2026, 7, 15, 11, 0, 0, 0, time.UTC)

	for i, used := range []string{"500", "300", "350"} {
		snap := core.NewUsageSnapshot("codex", "test")
		snap.Timestamp = start.Add(time.Duration(i) * time.Hour)
		applyCreditLimitDetails(&creditLimitDetails{Limit: "1000", Used: used}, &snap, "cli")
		p.applyCreditForecast(&snap, "test")
		if i == 1 {
			if _, ok := snap.Metrics["codex_credit_burn_rate"]; ok {
				t.Fatalf("did not expect a forecast immediately after a quota reset")
			}
		}
		if i == 2 {
			if rate := snap.Metrics["codex_credit_burn_rate"].Used; rate == nil || *rate <= 0 {
				t.Fatalf("expected a new positive forecast after post-reset usage, got %v", rate)
			}
		}
	}
}

func TestApplyRateLimitStatusIncludesCreditQuota(t *testing.T) {
	p := New()
	used := 95.0
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Metrics["codex_credit_percent_used"] = core.Metric{Used: &used, Unit: "%"}

	p.applyRateLimitStatus(&snap)
	if snap.Status != core.StatusNearLimit {
		t.Fatalf("expected near-limit status at 95%% credits used, got %s", snap.Status)
	}
}

func TestApplyCreditLimitOverride(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	applyCreditLimitDetails(&creditLimitDetails{Limit: "7500", Used: "3200"}, &snap, "cli")
	cap := 4000.0
	applyCreditLimitOverride(&cap, &snap)

	effective := snap.Metrics["codex_credit_limit"]
	if effective.Limit == nil || *effective.Limit != 4000 || effective.Remaining == nil || *effective.Remaining != 800 {
		t.Fatalf("unexpected effective metric: %+v", effective)
	}
	reported := snap.Metrics["codex_credit_reported_limit"]
	if reported.Limit == nil || *reported.Limit != 7500 {
		t.Fatalf("expected reported quota to remain 7500, got %+v", reported)
	}
	percent := snap.Metrics["codex_credit_percent_used"]
	if percent.Used == nil || *percent.Used != 80 {
		t.Fatalf("expected 80%% used against cap, got %+v", percent)
	}
	if snap.Raw["credit_limit_override_active"] != "true" {
		t.Fatalf("expected active override metadata, got %+v", snap.Raw)
	}
}

func TestApplyCreditLimitOverrideAlreadyExceeded(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	applyCreditLimitDetails(&creditLimitDetails{Limit: "7500", Used: "4500"}, &snap, "cli")
	cap := 4000.0
	applyCreditLimitOverride(&cap, &snap)

	percent := snap.Metrics["codex_credit_percent_used"]
	if percent.Used == nil || *percent.Used != 100 {
		t.Fatalf("expected clamped 100%%, got %+v", percent)
	}
	p := New()
	p.applyRateLimitStatus(&snap)
	if snap.Status != core.StatusLimited {
		t.Fatalf("expected LIMITED at personal cap, got %s", snap.Status)
	}
}

func TestApplyCreditLimitOverrideIgnoresHigherCap(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	applyCreditLimitDetails(&creditLimitDetails{Limit: "7500", Used: "1000"}, &snap, "cli")
	cap := 8000.0
	applyCreditLimitOverride(&cap, &snap)
	if got := *snap.Metrics["codex_credit_limit"].Limit; got != 7500 {
		t.Fatalf("expected reported limit to remain effective, got %.0f", got)
	}
	if snap.Raw["credit_limit_override_active"] != "false" {
		t.Fatalf("expected inactive override, got %+v", snap.Raw)
	}
}

func TestApplyCreditLimitOverrideRejectsInvalidCap(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	applyCreditLimitDetails(&creditLimitDetails{Limit: "7500", Used: "1000"}, &snap, "cli")
	cap := -1.0
	applyCreditLimitOverride(&cap, &snap)

	if got := *snap.Metrics["codex_credit_limit"].Limit; got != 7500 {
		t.Fatalf("expected invalid cap to leave reported limit unchanged, got %.0f", got)
	}
	if snap.Diagnostics["credit_limit_override"] == "" {
		t.Fatal("expected a non-fatal invalid-cap diagnostic")
	}
}

func TestApplyCreditLimitOverrideWithoutReportedQuota(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "codex-cli")
	cap := 4000.0
	applyCreditLimitOverride(&cap, &snap)

	if _, ok := snap.Metrics["codex_credit_limit"]; ok {
		t.Fatal("did not expect a synthetic credit quota")
	}
	if snap.Raw["credit_limit_override_configured"] != "4000" || snap.Raw["credit_limit_override_active"] != "false" {
		t.Fatalf("expected configured but inactive metadata, got %+v", snap.Raw)
	}
	if snap.Diagnostics["credit_limit_override"] == "" {
		t.Fatal("expected a missing-reported-quota diagnostic")
	}
}

func TestApplyCreditForecastKeepsObservingWhileDailyHistoryWorks(t *testing.T) {
	p := New()
	resetAt := time.Date(2026, 9, 1, 0, 0, 0, 0, time.Local)
	startDay := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	limit := 1000.0

	// Two polls an hour apart, both served by the daily-history path.
	for i, used := range []float64{100, 300} {
		at := time.Date(2026, 8, 3, 10, 0, 0, 0, time.Local).Add(time.Duration(i) * time.Hour)
		usedCopy := used
		snap := core.NewUsageSnapshot("codex", "test")
		snap.Timestamp = at
		snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &usedCopy, Unit: "credits"}
		snap.Resets["codex_credit_limit"] = resetAt
		snap.DailySeries = map[string][]core.TimePoint{
			codexCreditUsageDailySeriesKey: {
				{Date: formatCodexDay(startDay), Value: 50},
				{Date: formatCodexDay(startDay.AddDate(0, 0, 1)), Value: 50},
				{Date: formatCodexDay(at), Value: used - 100},
			},
		}
		p.applyCreditForecast(&snap, "test")
		if snap.Raw["credit_forecast_source"] != "account_daily_history" {
			t.Fatalf("poll %d: expected the daily path, got %q", i, snap.Raw["credit_forecast_source"])
		}
	}

	// The daily endpoint drops out. The observed-usage fallback must already
	// hold the samples collected while the daily path was working.
	used := 500.0
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = time.Date(2026, 8, 3, 12, 0, 0, 0, time.Local)
	snap.Metrics["codex_credit_limit"] = core.Metric{Limit: &limit, Used: &used, Unit: "credits"}
	p.applyCreditForecast(&snap, "test")

	if snap.Raw["credit_forecast_source"] != "observed_usage" {
		t.Fatalf("forecast source = %q, want observed_usage", snap.Raw["credit_forecast_source"])
	}
	rate := snap.Metrics["codex_credit_burn_rate"]
	if rate.Used == nil {
		t.Fatal("expected an observed burn rate from samples collected during the daily path")
	}
	// 100 -> 500 credits across two hours.
	if *rate.Used < 199.9 || *rate.Used > 200.1 {
		t.Fatalf("observed rate = %v, want about 200 credits/hour", rate.Used)
	}
}
