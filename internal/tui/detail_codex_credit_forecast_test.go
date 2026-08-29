package tui

import (
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestBuildDetailCodexCreditForecastSection(t *testing.T) {
	used := 2572.322
	limit := 7500.0
	rate := 200.0
	runout := 24.638
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"codex_credit_limit": {
				Used:  &used,
				Limit: &limit,
				Unit:  "credits",
			},
			"codex_credit_burn_rate":    {Used: &rate, Unit: "credits/hour"},
			"codex_credit_runout_hours": {Used: &runout, Unit: "h"},
		},
	}

	lines := buildDetailCodexCreditForecastSection(snap, 100)
	output := strings.Join(lines, "\n")
	for _, want := range []string{"Credit Usage", "2572 / 7500 credits (34%)", "200 credits/hour", "Credit Forecast", "1.0 days left"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected forecast output to contain %q, got %q", want, output)
		}
	}
}

func TestBuildDetailCodexCreditForecastSectionWithPersonalCap(t *testing.T) {
	used := 3200.0
	cap := 4000.0
	reported := 7500.0
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"codex_credit_limit":          {Used: &used, Limit: &cap, Unit: "credits"},
			"codex_credit_reported_limit": {Used: &used, Limit: &reported, Unit: "credits"},
		},
		Raw: map[string]string{"credit_limit_override_active": "true"},
	}

	output := strings.Join(buildDetailCodexCreditForecastSection(snap, 100), "\n")
	for _, want := range []string{"3200 / 4000 credits (80%)", "Personal Cap", "4000 credits (advisory)", "Reported Quota", "7500 credits"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got %q", want, output)
		}
	}
}

func TestBuildDetailCodexCreditForecastSectionWithReadModelMetrics(t *testing.T) {
	used := 3273.4
	cap := 1000.0
	reported := 7500.0
	snap := core.UsageSnapshot{Metrics: map[string]core.Metric{
		"codex_credit_limit":          {Used: &used, Limit: &cap, Unit: "credits"},
		"codex_credit_reported_limit": {Used: &used, Limit: &reported, Unit: "credits"},
	}}

	output := strings.Join(buildDetailCodexCreditForecastSection(snap, 100), "\n")
	if !strings.Contains(output, "3273 / 1000 credits (100%)") || !strings.Contains(output, "Personal Cap") || !strings.Contains(output, "Reported Quota") {
		t.Fatalf("expected cap labels from read-model metrics, got %q", output)
	}
}

func TestBuildDetailCodexCreditForecastSectionShowsDailyProjection(t *testing.T) {
	used := 35.0
	limit := 100.0
	dailyAverage := 11.666
	projected := 361.666
	reserve := -261.666
	snap := core.UsageSnapshot{Metrics: map[string]core.Metric{
		"codex_credit_limit":                      {Used: &used, Limit: &limit, Unit: "credits"},
		"codex_credit_daily_average":              {Used: &dailyAverage, Unit: "credits/day"},
		"codex_credit_projected_credits_at_reset": {Used: &projected, Unit: "credits"},
		"codex_credit_projected_reserve_at_reset": {Used: &reserve, Unit: "credits"},
	}}

	output := strings.Join(buildDetailCodexCreditForecastSection(snap, 100), "\n")
	for _, want := range []string{"Daily Average", "11.67 credits/day", "Expected at Reset", "361.67 credits", "Projected Deficit", "261.67 credits"} {
		if !strings.Contains(output, want) {
			t.Errorf("expected output to contain %q, got %q", want, output)
		}
	}
}

func TestCodexDailyCreditSeriesAppearsInUsageTrends(t *testing.T) {
	snap := core.UsageSnapshot{
		DailySeries: map[string][]core.TimePoint{
			"codex_credit_usage": {
				{Date: "2026-08-01", Value: 10},
				{Date: "2026-08-02", Value: 0},
				{Date: "2026-08-03", Value: 25},
			},
		},
	}

	output := strings.Join(buildProviderDailyTrendLines(snap, 100), "\n")
	if !strings.Contains(output, "Credits") {
		t.Fatalf("expected Codex credit daily trend, got %q", output)
	}
}
