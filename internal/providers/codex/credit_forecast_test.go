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
