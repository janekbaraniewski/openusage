package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/kimi_cli"
)

func TestKimiTileLocalAndWindowedMetrics(t *testing.T) {
	widget := kimi_cli.New().DashboardWidget()
	model := tileGaugeTestModel(time.Date(2026, 9, 19, 1, 0, 0, 0, time.UTC))
	for _, tc := range []struct {
		name    string
		metrics map[string]core.Metric
	}{
		{
			name: "all-time",
			metrics: map[string]core.Metric{
				"total_sessions":      {Used: float64Ptr(3)},
				"sessions_7d":         {Used: float64Ptr(2)},
				"total_input_tokens":  {Used: float64Ptr(80)},
				"total_output_tokens": {Used: float64Ptr(20)},
			},
		},
		{
			name: "windowed",
			metrics: map[string]core.Metric{
				"sessions_7d":                     {Used: float64Ptr(2)},
				"provider_kimi_cli_input_tokens":  {Used: float64Ptr(80)},
				"provider_kimi_cli_output_tokens": {Used: float64Ptr(20)},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := core.UsageSnapshot{ProviderID: kimi_cli.ID, Metrics: tc.metrics}
			if lines := model.buildTileGaugeLines(snap, widget, 60); len(lines) != 0 {
				t.Fatalf("local-only metrics rendered gauge placeholders: %q", lines)
			}
			lines, _ := buildTileCompactMetricSummaryLines(snap, widget, 60)
			if len(lines) != 2 {
				t.Fatalf("compact rows = %q, want Sessions and Tokens", lines)
			}
			rendered := strings.Join(lines, "\n")
			if !strings.Contains(rendered, "Sessions") || !strings.Contains(rendered, "Tokens") ||
				!strings.Contains(rendered, "in 80") || !strings.Contains(rendered, "out 20") {
				t.Errorf("missing local metrics in %q", rendered)
			}
		})
	}

	snap := core.UsageSnapshot{ProviderID: kimi_cli.ID, Metrics: map[string]core.Metric{
		"usage_five_hour": {Used: float64Ptr(46), Limit: float64Ptr(100), Unit: "%", Window: "5h"},
	}}
	if lines := model.buildTileGaugeLines(snap, widget, 60); len(lines) != 1 {
		t.Fatalf("quota gauge lines = %q, want one real gauge", lines)
	}
}

func TestKimiStaleQuotaVisibleWithOtherDataDisabled(t *testing.T) {
	now := time.Date(2026, 10, 5, 9, 36, 0, 0, time.UTC)
	model := tileGaugeTestModel(now)
	snap := core.NewUsageSnapshot("kimi_cli", "kimi")
	snap.Timestamp, snap.Status = now, core.StatusOK
	snap.Attributes["quota_state"] = "stale"
	snap.Attributes["quota_fetched_at"] = now.Add(-time.Hour).Format(time.RFC3339Nano)
	snap.Diagnostics["quota_error"] = "context deadline exceeded"
	snap.Metrics["usage_five_hour"] = makeUsageMetric(46, 100, "5h")
	snap.Metrics["usage_monthly"] = makeUsageMetric(69.8, 100, "30d")
	snap.Resets["usage_five_hour"] = now.Add(-time.Minute)
	snap.Resets["usage_monthly"] = now.Add(14 * 24 * time.Hour)
	widget := kimi_cli.New().DashboardWidget()
	widget.StandardSectionOrder = []core.DashboardStandardSection{core.DashboardSectionTopUsageProgress}
	for _, lines := range [][]string{
		model.buildTileBodyLines(snap, widget, providerDisplayInfo{}, 60, false, false),
		buildDetailGaugeLines(snap, widget, 60, 0.8, 0.9, now),
	} {
		rendered := strings.Join(lines, "\n")
		for _, text := range []string{"Stale quota", "05 Oct", "Kimi API timed out", "previous window", "46", "69.8"} {
			if !strings.Contains(rendered, text) {
				t.Errorf("missing %q in %q", text, rendered)
			}
		}
		if strings.Contains(rendered, "100% in") || strings.Contains(rendered, "by reset") {
			t.Errorf("stale quota must not project current usage: %q", rendered)
		}
	}
	snap.Attributes["quota_state"] = "unavailable"
	snap.Metrics = map[string]core.Metric{}
	snap.Diagnostics = map[string]string{"quota": "access token expired"}
	if rendered := strings.Join(model.buildTileGaugeLines(snap, widget, 60), "\n"); !strings.Contains(rendered, "Quota unavailable") || !strings.Contains(rendered, "access token expired") {
		t.Fatalf("unavailable quota has no visible reason: %q", rendered)
	}
}
