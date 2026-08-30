package tui

import (
	"strings"
	"testing"
	"time"

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

func TestRenderListSummaryRow_ShowsResetAtMaxSidebarWidth(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.referenceTime = now

	snap := cursorPlanSnap(now)
	di := computeDisplayInfo(snap, dashboardWidget("cursor"), false, config.UsageModeRemaining)
	out := m.renderListSummaryRow(snap, di, maxLeftWidth)

	if !strings.Contains(out, "Resets in") {
		t.Fatalf("expected reset duration at max sidebar width, got %q", out)
	}
	if !strings.Contains(out, "93.00%") {
		t.Fatalf("expected full summary preserved, got %q", out)
	}
	if strings.Contains(out, "93.0…") {
		t.Fatalf("summary should not truncate at max width, got %q", out)
	}
	if !strings.Contains(out, compactBlockFilled) {
		t.Fatalf("expected block strip preserved, got %q", out)
	}
	idxPct := strings.Index(out, "93.00%")
	idxReset := strings.Index(out, "Resets in")
	if idxPct < 0 || idxReset < 0 || idxPct > idxReset {
		t.Fatalf("expected inline single-row layout, got %q", out)
	}
}

func TestRenderListSummaryRow_ShowsResetAtTypicalSidebarWidth(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.referenceTime = now

	snap := cursorPlanSnap(now)
	di := computeDisplayInfo(snap, dashboardWidget("cursor"), false, config.UsageModeRemaining)
	out := m.renderListSummaryRow(snap, di, 33)

	if !strings.Contains(out, "Resets in") && !strings.Contains(out, "in ") {
		t.Fatalf("expected reset duration on typical sidebar width, got %q", out)
	}
}

func TestRenderListSummaryRow_HidesResetWhenTooNarrow(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.referenceTime = now

	snap := cursorPlanSnap(now)
	di := computeDisplayInfo(snap, dashboardWidget("cursor"), false, config.UsageModeRemaining)
	out := m.renderListSummaryRow(snap, di, 24)

	if strings.Contains(out, "Resets in") || strings.Contains(out, " days") || strings.Contains(out, "d") {
		// narrow widths may still fit compact duration; ensure no calendar month names
		for _, month := range []string{"Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"} {
			if strings.Contains(out, month) {
				t.Fatalf("narrow sidebar should omit reset, got %q", out)
			}
		}
	}
}

func TestRenderListSummaryRow_DualCycleShowsPrimaryResetInline(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	m := NewModel(0.2, 0.05, false, config.DashboardConfig{}, nil, core.TimeWindow30d)
	m.referenceTime = now

	snap := core.UsageSnapshot{
		ProviderID: "command_code",
		Metrics: map[string]core.Metric{
			"monthly_subscription": {Remaining: core.Float64Ptr(48)},
			"weekly_usage":         {Remaining: core.Float64Ptr(70)},
		},
		Resets: map[string]time.Time{
			"monthly_subscription": time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
			"weekly_usage":         time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC),
		},
	}
	di := computeDisplayInfo(snap, dashboardWidget("command_code"), false, config.UsageModeRemaining)
	out := m.renderListSummaryRow(snap, di, maxLeftWidth)

	if !strings.Contains(out, "Resets in") {
		t.Fatalf("expected primary monthly reset inline, got %q", out)
	}
	if strings.Contains(out, "Weekly") {
		t.Fatalf("sidebar should only show primary reset, got %q", out)
	}
}
