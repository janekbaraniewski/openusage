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
