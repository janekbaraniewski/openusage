package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderUsageGaugeWithProjection(t *testing.T) {
	const usedPercent = 50.0
	const overLimitPercent = 100.0
	const width = 20
	const warn = 0.30
	const crit = 0.15
	resetIn := 30 * time.Minute

	cases := []struct {
		name           string
		usedPercent    float64
		paceFraction   float64
		resetIn        time.Duration
		wantContains   []string
		wantNotContain []string
	}{
		{
			name:           "happy_path",
			usedPercent:    usedPercent,
			paceFraction:   0.05, // 5%/min → 100% in 10m, well inside the 30m window
			resetIn:        resetIn,
			wantContains:   []string{"resets in", "projected 100% in"},
			wantNotContain: []string{"by reset"},
		},
		{
			// Pace would overshoot the window: 1%/min, 50% remaining → 50m to
			// 100%, but only 30m to reset. Should switch to "~N% by reset".
			name:           "overshoots_window",
			usedPercent:    usedPercent,
			paceFraction:   0.01,
			resetIn:        resetIn,
			wantContains:   []string{"resets in", "projected ~80% by reset"},
			wantNotContain: []string{"100% in"},
		},
		{
			// Pace exactly hits reset: 50% used, 50% remaining, 1%/min,
			// 50m to 100%, resetIn=50m → projected time == reset time, so
			// we keep the "100% in" wording (only > triggers the switch).
			name:           "projection_equals_reset",
			usedPercent:    usedPercent,
			paceFraction:   0.01,
			resetIn:        50 * time.Minute,
			wantContains:   []string{"resets in", "projected 100% in"},
			wantNotContain: []string{"by reset"},
		},
		{
			// Edge case: projected % at reset rounds up to 100, but the
			// branch should never claim "~100% by reset" (it would
			// contradict why we picked this branch in the first place).
			// minutesTo100 = 90/1.499 ≈ 60.04m > resetIn (60m) → branch
			// taken; projectedPct = 10 + 1.499*60 = 99.94 → rounds to 100
			// → capped to 99.
			name:           "by_reset_caps_below_100",
			usedPercent:    10.0,
			paceFraction:   0.01499,
			resetIn:        60 * time.Minute,
			wantContains:   []string{"resets in", "projected ~99% by reset"},
			wantNotContain: []string{"~100%", "100% in"},
		},
		{
			name:           "nan_pace",
			usedPercent:    usedPercent,
			paceFraction:   math.NaN(),
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "inf_pace",
			usedPercent:    usedPercent,
			paceFraction:   math.Inf(1),
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "zero_pace",
			usedPercent:    usedPercent,
			paceFraction:   0,
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "negative_pace",
			usedPercent:    usedPercent,
			paceFraction:   -0.5,
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "over_limit",
			usedPercent:    overLimitPercent,
			paceFraction:   0.01,
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "reset_only",
			usedPercent:    usedPercent,
			paceFraction:   0,
			resetIn:        resetIn,
			wantContains:   []string{"resets in"},
			wantNotContain: []string{"projected"},
		},
		{
			name:           "pace_only",
			usedPercent:    usedPercent,
			paceFraction:   0.01,
			resetIn:        0,
			wantContains:   []string{"projected"},
			wantNotContain: []string{"resets in"},
		},
		{
			name:           "neither",
			usedPercent:    usedPercent,
			paceFraction:   0,
			resetIn:        0,
			wantNotContain: []string{"resets in", "projected"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := RenderUsageGaugeWithProjection(tc.usedPercent, width, warn, crit, tc.paceFraction, tc.resetIn)
			if out == "" {
				t.Fatal("expected non-empty output")
			}
			for _, want := range tc.wantContains {
				if !strings.Contains(out, want) {
					t.Errorf("expected output to contain %q, got %q", want, out)
				}
			}
			for _, notWant := range tc.wantNotContain {
				if strings.Contains(out, notWant) {
					t.Errorf("expected output to NOT contain %q, got %q", notWant, out)
				}
			}
		})
	}
}

func TestRenderStackedUsageGauge_TwoSegments(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 30, Color: lipgloss.Color("#00ff00")},
		{Percent: 20, Color: lipgloss.Color("#ffaa00")},
	}
	out := RenderStackedUsageGauge(segments, 50, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "50.0%") {
		t.Fatalf("output should contain '50.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_ZeroPercent(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 0, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, 0, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "0.0%") {
		t.Fatalf("output should contain '0.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_HundredPercent(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 60, Color: lipgloss.Color("#ff0000")},
		{Percent: 40, Color: lipgloss.Color("#0000ff")},
	}
	out := RenderStackedUsageGauge(segments, 100, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "100.0%") {
		t.Fatalf("output should contain '100.0%%', got %q", out)
	}
	// At 100%, the track character should not appear.
	if strings.Contains(out, "░") {
		t.Fatal("100% gauge should not contain empty track characters")
	}
}

func TestRenderStackedUsageGauge_SingleSegment(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 75, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, 75, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "75.0%") {
		t.Fatalf("output should contain '75.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_NegativeRendersNA(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 50, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, -1, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "N/A") {
		t.Fatalf("negative totalPercent should render N/A, got %q", out)
	}
}

func TestRenderShimmerGauge(t *testing.T) {
	out := RenderShimmerGauge(20, 0)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "···") {
		t.Fatalf("shimmer gauge should contain loading indicator, got %q", out)
	}
	// Verify it renders at different frames without panic.
	for f := 0; f < 30; f++ {
		if RenderShimmerGauge(20, f) == "" {
			t.Fatalf("empty output at frame %d", f)
		}
	}
}

func TestRenderShimmerGauge_NarrowWidth(t *testing.T) {
	out := RenderShimmerGauge(2, 0)
	if out == "" {
		t.Fatal("expected non-empty output for narrow width")
	}
}

func TestRenderStackedUsageGauge_NarrowWidth(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 30, Color: lipgloss.Color("#00ff00")},
		{Percent: 20, Color: lipgloss.Color("#ffaa00")},
	}
	out := RenderStackedUsageGauge(segments, 50, 2)
	if out == "" {
		t.Fatal("expected non-empty output for narrow width")
	}
	if !strings.Contains(out, "50.0%") {
		t.Fatalf("narrow width output should still contain '50.0%%', got %q", out)
	}
}
