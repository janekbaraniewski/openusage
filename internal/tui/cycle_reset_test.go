package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestFormatCycleResetDuration(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{5 * 24 * time.Hour, "5 days"},
		{2 * 24 * time.Hour, "2 days"},
		{47*time.Hour + 30*time.Minute, "1d 23h"},
		{36 * time.Hour, "1d 12h"},
		{90 * time.Minute, "1h 30m"},
	}

	for _, tc := range tests {
		got := formatCycleResetDuration(tc.d)
		if got != tc.want {
			t.Fatalf("formatCycleResetDuration(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}

func TestFormatCycleResetSchedule_CursorMonthly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	reset := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor",
		Timestamp:  now,
		Metrics: map[string]core.Metric{
			"plan_percent_used": {Used: core.Float64Ptr(7), Remaining: core.Float64Ptr(93), Window: "monthly"},
		},
		Resets: map[string]time.Time{
			"plan_percent_used": reset,
			"rolling_usage":     now.Add(3 * time.Hour),
		},
	}

	got := formatCycleResetSchedule(snap, now)
	if !strings.HasPrefix(got, "Resets in ") {
		t.Fatalf("schedule = %q, want Resets in ...", got)
	}
	if strings.Contains(got, "Sep") {
		t.Fatalf("schedule should not contain calendar date, got %q", got)
	}
}

func TestFormatCycleResetSchedule_AntigravityWeekly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	weekly := time.Date(2026, 9, 6, 3, 31, 0, 0, time.UTC)
	snap := core.UsageSnapshot{
		ProviderID: "antigravity",
		Metrics: map[string]core.Metric{
			"quota_gemini_weekly": {Remaining: core.Float64Ptr(90)},
			"quota_gemini_5h":     {Remaining: core.Float64Ptr(80)},
		},
		Resets: map[string]time.Time{
			"quota_gemini_weekly": weekly,
			"quota_gemini_5h":     now.Add(2 * time.Hour),
		},
	}

	got := formatCycleResetSchedule(snap, now)
	if !strings.HasPrefix(got, "Resets in ") {
		t.Fatalf("schedule = %q, want Resets in ...", got)
	}
}

func TestFormatCycleResetSchedule_CommandCodeMonthlyAndWeekly(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	snap := core.UsageSnapshot{
		ProviderID: "command_code",
		Metrics: map[string]core.Metric{
			"monthly_subscription": {Remaining: core.Float64Ptr(48)},
			"weekly_usage":         {Remaining: core.Float64Ptr(70)},
			"five_hour_usage":      {Remaining: core.Float64Ptr(90)},
		},
		Resets: map[string]time.Time{
			"monthly_subscription": time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC),
			"weekly_usage":         time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC),
			"five_hour_usage":      now.Add(4 * time.Hour),
		},
	}

	got := formatCycleResetSchedule(snap, now)
	if !strings.Contains(got, "Monthly resets in") || !strings.Contains(got, "Weekly resets in") {
		t.Fatalf("schedule = %q, want monthly and weekly duration labels", got)
	}

	compact := formatCycleResetScheduleCompact(snap, now)
	if !strings.HasPrefix(compact, "Resets in ") || !strings.Contains(compact, " · ") {
		t.Fatalf("compact = %q, want combined duration reset line", compact)
	}
}

func TestFormatCycleResetSchedule_ImminentCountdown(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	reset := now.Add(36 * time.Hour)
	snap := core.UsageSnapshot{
		ProviderID: "cursor",
		Metrics: map[string]core.Metric{
			"plan_percent_used": {Remaining: core.Float64Ptr(50), Window: "monthly"},
		},
		Resets: map[string]time.Time{"plan_percent_used": reset},
	}

	got := formatCycleResetSchedule(snap, now)
	if got != "Resets in 1d 12h" {
		t.Fatalf("expected sub-2-day precision, got %q", got)
	}
}

func TestRenderDetailContent_ShowsCycleResetInHeader(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	snap := cursorPlanSnap(now)
	out := RenderDetailContent(snap, now, 100, 0.2, 0.05, 0, core.TimeWindow30d, false, config.UsageModeRemaining)

	header := strings.Join(strings.Split(out, "\n")[:2], "\n")
	if !strings.Contains(header, "Resets in") {
		t.Fatalf("expected reset duration in detail header, got:\n%s", header)
	}
	if strings.Contains(header, "Sep") {
		t.Fatalf("detail header should not show calendar date, got:\n%s", header)
	}
	if strings.Contains(header, "Last refreshed") {
		t.Fatalf("fresh snapshot should not show last refreshed in header, got:\n%s", header)
	}
}

func TestFormatLastRefreshedIfStale(t *testing.T) {
	now := time.Date(2026, 8, 30, 12, 0, 0, 0, time.UTC)
	if got := formatLastRefreshedIfStale(now.Add(-10*time.Second), now); got != "" {
		t.Fatalf("fresh timestamp should be hidden, got %q", got)
	}
	if got := formatLastRefreshedIfStale(now.Add(-10*time.Minute), now); got == "" {
		t.Fatal("stale timestamp should be shown")
	}
}
