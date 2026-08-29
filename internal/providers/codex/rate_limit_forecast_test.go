package codex

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestApplyRateLimitForecastUsesInferredPeriodStart(t *testing.T) {
	p := New()
	observedAt := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	resetAt := observedAt.Add(6*24*time.Hour + 17*time.Hour)
	used := 25.0
	limit := 100.0

	snap := core.NewUsageSnapshot("codex", "test")
	snap.Timestamp = observedAt
	snap.Metrics["rate_limit_primary"] = core.Metric{
		Limit:  &limit,
		Used:   &used,
		Unit:   "%",
		Window: "7d",
	}
	snap.Resets["rate_limit_primary"] = resetAt

	p.applyRateLimitForecast(&snap)

	rate := snap.Metrics["codex_rate_limit_burn_rate"]
	if rate.Used == nil || *rate.Used < 3.57 || *rate.Used > 3.58 {
		t.Fatalf("expected about 3.571 percentage points/hour, got %v", rate.Used)
	}
	if rate.Unit != "%/hour" || rate.Window != "current-period average" {
		t.Fatalf("unexpected burn-rate metric: %+v", rate)
	}

	runout := snap.Metrics["codex_rate_limit_runout_hours"]
	if runout.Used == nil || *runout.Used < 20.99 || *runout.Used > 21.01 {
		t.Fatalf("expected 21 hours to run out, got %v", runout.Used)
	}
	if snap.Raw["rate_limit_forecast_source"] != "inferred_period_start" {
		t.Fatalf("expected inferred forecast source, got %q", snap.Raw["rate_limit_forecast_source"])
	}
	wantStart := resetAt.Add(-7 * 24 * time.Hour).UTC().Format(time.RFC3339)
	if snap.Raw["rate_limit_forecast_period_start"] != wantStart {
		t.Fatalf("expected period start %q, got %q", wantStart, snap.Raw["rate_limit_forecast_period_start"])
	}
}

func TestApplyRateLimitForecastRejectsIncompleteWindow(t *testing.T) {
	p := New()
	observedAt := time.Date(2026, 8, 15, 16, 0, 0, 0, time.UTC)
	used := 25.0
	limit := 100.0

	tests := []struct {
		name   string
		window string
		reset  time.Time
	}{
		{name: "missing reset", window: "7d"},
		{name: "unknown window", window: "current-period", reset: observedAt.Add(5 * 24 * time.Hour)},
		{name: "stale reset", window: "7d", reset: observedAt.Add(-time.Hour)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("codex", "test")
			snap.Timestamp = observedAt
			snap.Metrics["rate_limit_primary"] = core.Metric{Limit: &limit, Used: &used, Unit: "%", Window: tt.window}
			if !tt.reset.IsZero() {
				snap.Resets["rate_limit_primary"] = tt.reset
			}

			p.applyRateLimitForecast(&snap)
			if _, ok := snap.Metrics["codex_rate_limit_burn_rate"]; ok {
				t.Fatal("did not expect a burn-rate forecast")
			}
			if _, ok := snap.Metrics["codex_rate_limit_runout_hours"]; ok {
				t.Fatal("did not expect a runout forecast")
			}
		})
	}
}

func TestParseRateLimitWindow(t *testing.T) {
	tests := []struct {
		window string
		want   time.Duration
	}{
		{window: "7d", want: 7 * 24 * time.Hour},
		{window: "1d12h", want: 36 * time.Hour},
		{window: "rolling-5h", want: 5 * time.Hour},
		{window: "30m", want: 30 * time.Minute},
	}
	for _, tt := range tests {
		got, ok := parseRateLimitWindow(tt.window)
		if !ok || got != tt.want {
			t.Errorf("parseRateLimitWindow(%q) = %v, %v; want %v, true", tt.window, got, ok, tt.want)
		}
	}
}
