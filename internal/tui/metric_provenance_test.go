package tui

import (
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFormatMetricMetaTag(t *testing.T) {
	tag := formatMetricMetaTag(core.Metric{
		Unit:   "USD",
		Window: "7d",
		Source: core.MetricSourceEstimated,
	})
	if tag != "[7d · estimated]" {
		t.Fatalf("formatMetricMetaTag() = %q, want %q", tag, "[7d · estimated]")
	}
}

func TestDeriveProviderDailyCostPointsDoesNotEstimateFromWeekCost(t *testing.T) {
	points, observed := deriveProviderDailyCostPoints(providerCostEntry{
		name:      "Claude Code",
		weekCost:  42,
		todayCost: 0,
	}, &timeSeriesGroup{
		providerName: "Claude Code",
		series: map[string][]core.TimePoint{
			"tokens": {
				{Date: "2026-03-08", Value: 100},
				{Date: "2026-03-09", Value: 200},
			},
		},
	}, time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC))

	if observed {
		t.Fatal("expected derived provider series without observed cost data to remain unobserved")
	}
	if len(points) != 0 {
		t.Fatalf("deriveProviderDailyCostPoints() returned %v, want no points", points)
	}
}
