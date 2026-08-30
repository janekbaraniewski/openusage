package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestListSummaryColor_GaugeThresholds(t *testing.T) {
	const warn = 0.30
	const crit = 0.15

	tests := []struct {
		name     string
		pct      float64
		usedMode bool
		want     lipgloss.Color
	}{
		{"remaining healthy", 85, false, colorOK},
		{"remaining medium", 40, false, colorYellow},
		{"remaining low", 20, false, colorPeach},
		{"remaining critical", 5, false, colorCrit},
		{"used healthy", 20, true, colorOK},
		{"used medium", 60, true, colorYellow},
		{"used high", 80, true, colorPeach},
		{"used critical", 95, true, colorCrit},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := listSummaryColor(tc.pct, tc.usedMode, warn, crit, core.StatusOK)
			if got != tc.want {
				t.Fatalf("listSummaryColor(%v, used=%v) = %q, want %q", tc.pct, tc.usedMode, got, tc.want)
			}
		})
	}
}

func TestListSummaryColor_StatusFallback(t *testing.T) {
	if got := listSummaryColor(-1, false, 0.3, 0.15, core.StatusLimited); got != colorPeach {
		t.Fatalf("limited status = %q, want peach", got)
	}
	if got := listSummaryColor(-1, false, 0.3, 0.15, core.StatusOK); got != colorText {
		t.Fatalf("ok status without gauge = %q, want text", got)
	}
}

func TestRenderCompactBlockStrip(t *testing.T) {
	tests := []struct {
		name    string
		percent float64
		want    string
	}{
		{"high", 78.8, "▰▰▰▰▱"},
		{"medium", 52.99, "▰▰▰▱▱"},
		{"empty", 0, "▱▱▱▱▱"},
		{"full", 100, "▰▰▰▰▰"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderCompactBlockStrip(tc.percent, 5, colorOK)
			filled := strings.Count(tc.want, "▰")
			empty := strings.Count(tc.want, "▱")
			gotFilled := strings.Count(out, compactBlockFilled)
			gotEmpty := strings.Count(out, compactBlockEmpty)
			if gotFilled != filled || gotEmpty != empty {
				t.Fatalf("percent %.2f: got %d filled / %d empty, want %d / %d (out=%q)", tc.percent, gotFilled, gotEmpty, filled, empty, out)
			}
		})
	}
}

func TestRenderListSummary_IncludesBlockStrip(t *testing.T) {
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-test",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used": {Used: core.Float64Ptr(22), Remaining: core.Float64Ptr(78)},
		},
	}
	out := m.renderListSummary("78.00%", 78, snap)
	if !strings.Contains(out, compactBlockFilled) || !strings.Contains(out, compactBlockEmpty) {
		t.Fatalf("expected block strip in summary, got %q", out)
	}
	if !strings.Contains(out, "78.00%") {
		t.Fatalf("expected summary text preserved, got %q", out)
	}
	idxStrip := strings.Index(out, compactBlockFilled)
	idxPct := strings.Index(out, "78.00%")
	if idxStrip < 0 || idxPct < 0 || idxStrip > idxPct {
		t.Fatalf("strip should precede percent, got %q", out)
	}
}
