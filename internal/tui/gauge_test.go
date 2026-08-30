package tui

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
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

func TestRenderMiniUsageGauge(t *testing.T) {
	out := RenderMiniUsageGauge(30.0, 10)
	if out == "" {
		t.Fatal("expected non-empty output for 30% used")
	}

	outWarn := RenderMiniUsageGauge(60.0, 10)
	if outWarn == "" {
		t.Fatal("expected non-empty output for 60% used")
	}

	outCrit := RenderMiniUsageGauge(90.0, 10)
	if outCrit == "" {
		t.Fatal("expected non-empty output for 90% used")
	}

	outNeg := RenderMiniUsageGauge(-1, 10)
	if !strings.Contains(outNeg, "░") {
		t.Errorf("expected track characters for negative percent, got %q", outNeg)
	}
}

func TestUsageGaugeTrackColor(t *testing.T) {
	cases := []struct {
		used float64
		want lipgloss.Color
	}{
		{0, colorTeal},
		{-1, colorTeal},
		{1, colorSurface1},
		{25, colorSurface1},
		{49.9, colorSurface1},
		{50, colorSurface2},
		{74.9, colorSurface2},
		{75, colorSurface1},
		{100, colorSurface1},
	}
	for _, tc := range cases {
		got := usageGaugeTrackColor(tc.used)
		if got != tc.want {
			t.Errorf("usageGaugeTrackColor(%v) = %v, want %v", tc.used, got, tc.want)
		}
	}
}

func TestRenderUsageGauge_LowUsedShowsSmallFill(t *testing.T) {
	out := RenderUsageGauge(1, 20, 0.30, 0.15)
	if !strings.Contains(out, "1.00%") {
		t.Fatalf("expected 1.00%% label, got %q", out)
	}
	if !strings.Contains(out, "░") {
		t.Fatalf("expected neutral track characters, got %q", out)
	}
	// Partial or full block char indicates visible fill separate from track.
	hasFill := strings.ContainsAny(out, "█▏▎▍▌▋▊▉")
	if !hasFill {
		t.Fatalf("expected visible fill at 1%% used, got %q", out)
	}
}

func TestRenderUsageGauge_ZeroUsedShowsHealthyTrack(t *testing.T) {
	remainingGauge := RenderGauge(100, 20, 0.30, 0.15)
	usedGauge := RenderUsageGauge(0, 20, 0.30, 0.15)

	if !strings.Contains(usedGauge, "0.00%") {
		t.Fatalf("expected 0.00%% label, got %q", usedGauge)
	}
	if usedGauge == remainingGauge {
		t.Fatal("expected used-mode 0%% gauge to differ from remaining-mode 100%% gauge")
	}
	if !strings.Contains(usedGauge, "░") {
		t.Fatalf("expected track characters in zero-used gauge, got %q", usedGauge)
	}
}

func TestRenderQuotaStatusAndTimerLineWithMode(t *testing.T) {
	now := time.Now()
	resetAt := now.Add(2 * time.Hour)

	// Remaining mode (default)
	remLine := RenderQuotaStatusAndTimerLineWithMode(75.0, resetAt, now, false)
	if !strings.Contains(remLine, "75.00% remaining") {
		t.Errorf("expected 75.00%% remaining, got %q", remLine)
	}
	if !strings.Contains(remLine, "Resets in") {
		t.Errorf("expected Resets in tag, got %q", remLine)
	}

	// Used mode
	usedLine := RenderQuotaStatusAndTimerLineWithMode(75.0, resetAt, now, true)
	if !strings.Contains(usedLine, "25.00% used") {
		t.Errorf("expected 25.00%% used, got %q", usedLine)
	}
	if !strings.Contains(usedLine, "Resets in") {
		t.Errorf("expected Resets in tag, got %q", usedLine)
	}
}

func TestSnapshotStatusBadge_SpecificLimits(t *testing.T) {
	zero := 0.0
	hundred := 100.0

	// Weekly limit
	wkSnap := core.UsageSnapshot{
		Status: core.StatusLimited,
		Metrics: map[string]core.Metric{
			"weekly_usage": {Remaining: &zero, Window: "7d"},
		},
	}
	wkBadge := SnapshotStatusBadge(wkSnap)
	if !strings.Contains(wkBadge, "WEEKLY LIMIT") {
		t.Errorf("expected WEEKLY LIMIT in badge, got %q", wkBadge)
	}

	// Monthly limit
	moSnap := core.UsageSnapshot{
		Status: core.StatusLimited,
		Metrics: map[string]core.Metric{
			"monthly_usage_pct": {Used: &hundred, Unit: "%", Window: "30d"},
		},
	}
	moBadge := SnapshotStatusBadge(moSnap)
	if !strings.Contains(moBadge, "MONTHLY LIMIT") {
		t.Errorf("expected MONTHLY LIMIT in badge, got %q", moBadge)
	}

	// 5h limit
	fiveSnap := core.UsageSnapshot{
		Status: core.StatusLimited,
		Metrics: map[string]core.Metric{
			"rolling_usage": {Remaining: &zero, Window: "5h"},
		},
	}
	fiveBadge := SnapshotStatusBadge(fiveSnap)
	if !strings.Contains(fiveBadge, "5H LIMIT") {
		t.Errorf("expected 5H LIMIT in badge, got %q", fiveBadge)
	}

	// Generic limit fallback
	genSnap := core.UsageSnapshot{
		Status: core.StatusLimited,
	}
	genBadge := SnapshotStatusBadge(genSnap)
	if !strings.Contains(genBadge, "LIMIT") {
		t.Errorf("expected LIMIT in badge, got %q", genBadge)
	}

	// Antigravity with Gemini active: 5h limit hit (0% remaining), but weekly limit has 37.87% remaining.
	// Claude weekly has 0% remaining, but active model is Gemini.
	// Must display 5H LIMIT, NOT WEEKLY LIMIT!
	thirtySeven := 37.87
	agSnap := core.UsageSnapshot{
		ProviderID: "antigravity",
		Status:     core.StatusLimited,
		Attributes: map[string]string{
			"model":           "Gemini 3.7 Flash (High)",
			"claude_disabled": "true",
		},
		Metrics: map[string]core.Metric{
			"quota_gemini_5h":     {Remaining: &zero, Window: "5h"},
			"quota_gemini_weekly": {Remaining: &thirtySeven, Window: "7d"},
			"quota_claude_weekly": {Remaining: &zero, Window: "7d"},
			"quota_3p_weekly":     {Remaining: &zero, Window: "7d"},
		},
	}
	agBadge := SnapshotStatusBadge(agSnap)
	if !strings.Contains(agBadge, "5H LIMIT") {
		t.Errorf("expected 5H LIMIT in badge when Gemini weekly has remaining quota, got %q", agBadge)
	}
	if strings.Contains(agBadge, "WEEKLY LIMIT") {
		t.Errorf("did NOT expect WEEKLY LIMIT when Gemini weekly has 37.87%% remaining, got %q", agBadge)
	}
}

func TestUsageGaugeColor_Tiers(t *testing.T) {
	const warn = 0.20
	const crit = 0.05

	// 77.88% used is high usage — must be orange (colorPeach)
	if c := usageGaugeColor(77.88, warn, crit); c != colorPeach {
		t.Errorf("expected colorPeach (orange) for 77.88%% used, got %v", c)
	}

	// 30% used is low/healthy — green (colorOK)
	if c := usageGaugeColor(30.0, warn, crit); c != colorOK {
		t.Errorf("expected colorOK (green) for 30%% used, got %v", c)
	}

	// 60% used is medium — yellow (colorYellow)
	if c := usageGaugeColor(60.0, warn, crit); c != colorYellow {
		t.Errorf("expected colorYellow for 60%% used, got %v", c)
	}

	// 95% used is critical — red (colorCrit)
	if c := usageGaugeColor(95.0, warn, crit); c != colorCrit {
		t.Errorf("expected colorCrit for 95%% used, got %v", c)
	}
}
