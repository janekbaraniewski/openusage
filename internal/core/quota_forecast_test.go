package core

import (
	"testing"
	"time"
)

func TestApplyQuotaForecastSelectsSoonestValidQuota(t *testing.T) {
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	snap := NewUsageSnapshot("test", "account")
	snap.Timestamp = observedAt

	fastUsed := 25.0
	fastLimit := 100.0
	snap.Metrics["fast_quota"] = Metric{
		Used: &fastUsed, Limit: &fastLimit, Unit: "percent", Window: "5h",
	}
	snap.Resets["fast_quota_reset"] = observedAt.Add(3 * time.Hour)

	slowUsed := 10.0
	slowLimit := 100.0
	snap.Metrics["slow_quota"] = Metric{
		Used: &slowUsed, Limit: &slowLimit, Unit: "%", Window: "7d",
	}
	snap.Resets["slow_quota"] = observedAt.Add(5 * 24 * time.Hour)

	ApplyQuotaForecast(&snap)

	if got := snap.Raw["quota_forecast_metric"]; got != "fast_quota" {
		t.Fatalf("expected fast_quota forecast source, got %q", got)
	}
	rate := snap.Metrics["quota_burn_rate"]
	if rate.Used == nil || *rate.Used < 12.49 || *rate.Used > 12.51 {
		t.Fatalf("expected 12.5 percentage points/hour, got %v", rate.Used)
	}
	if rate.Unit != "%/hour" {
		t.Fatalf("expected normalized percentage rate unit, got %q", rate.Unit)
	}
	runout := snap.Metrics["quota_runout_hours"]
	if runout.Used == nil || *runout.Used < 5.99 || *runout.Used > 6.01 {
		t.Fatalf("expected 6 hours to run out, got %v", runout.Used)
	}
}

func TestApplyQuotaForecastUsesBillingCycleStart(t *testing.T) {
	observedAt := time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	periodStart := observedAt.Add(-10 * time.Hour)
	resetAt := observedAt.Add(12 * time.Hour)
	used := 50.0
	limit := 100.0

	snap := NewUsageSnapshot("test", "account")
	snap.Timestamp = observedAt
	snap.Metrics["monthly_usage"] = Metric{
		Used: &used, Limit: &limit, Unit: "credits", Window: "billing-cycle",
	}
	snap.Resets["monthly_usage_reset"] = resetAt
	snap.Raw["billing_cycle_start"] = periodStart.Format(time.RFC3339)

	ApplyQuotaForecast(&snap)

	rate := snap.Metrics["quota_burn_rate"]
	if rate.Used == nil || *rate.Used < 4.99 || *rate.Used > 5.01 {
		t.Fatalf("expected 5 credits/hour, got %v", rate.Used)
	}
	if rate.Unit != "credits/hour" {
		t.Fatalf("expected credits/hour, got %q", rate.Unit)
	}
	if got := snap.Raw["quota_forecast_reset_at"]; got != resetAt.Format(time.RFC3339) {
		t.Fatalf("expected reset %q, got %q", resetAt.Format(time.RFC3339), got)
	}
}

func TestApplyQuotaForecastRequiresResetAndWindow(t *testing.T) {
	used := 20.0
	limit := 100.0
	snap := NewUsageSnapshot("test", "account")
	snap.Timestamp = time.Date(2026, 8, 15, 12, 0, 0, 0, time.UTC)
	snap.Metrics["missing_reset"] = Metric{Used: &used, Limit: &limit, Unit: "%", Window: "7d"}
	snap.Metrics["unknown_window"] = Metric{Used: &used, Limit: &limit, Unit: "%", Window: "month"}
	snap.Resets["unknown_window"] = snap.Timestamp.Add(24 * time.Hour)

	ApplyQuotaForecast(&snap)

	if _, ok := snap.Metrics["quota_burn_rate"]; ok {
		t.Fatal("did not expect a forecast without a fixed window or period start")
	}
	if _, ok := snap.Metrics["quota_runout_hours"]; ok {
		t.Fatal("did not expect a runout without a fixed window or period start")
	}
}
