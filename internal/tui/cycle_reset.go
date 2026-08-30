package tui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

type cycleResetTier int

const (
	cycleResetMonthly cycleResetTier = iota
	cycleResetWeekly
)

type cycleResetEntry struct {
	tier cycleResetTier
	at   time.Time
}

func isShortWindowResetKey(key string) bool {
	k := strings.ToLower(strings.TrimSuffix(key, "_reset"))
	short := []string{
		"five_hour", "5h", "rolling", "billing_block", "usage_five_hour",
		"usage_one_day", "rate_limit", "rpm", "tpm", "rpd", "tpd",
	}
	for _, s := range short {
		if strings.Contains(k, s) {
			return true
		}
	}
	if strings.HasPrefix(k, "gh_") {
		return true
	}
	if strings.HasSuffix(k, "_5h") {
		return true
	}
	switch k {
	case "rolling_usage", "five_hour_usage", "key_expires":
		return true
	}
	return false
}

func cycleResetTierForKey(key string, snap core.UsageSnapshot) (cycleResetTier, bool) {
	if isShortWindowResetKey(key) {
		return 0, false
	}

	k := strings.ToLower(strings.TrimSuffix(key, "_reset"))
	if met, ok := snap.Metrics[k]; ok {
		w := strings.ToLower(strings.TrimSpace(met.Window))
		switch {
		case strings.Contains(w, "5h"), strings.Contains(w, "rolling"), w == "1d", strings.Contains(w, "hour"):
			return 0, false
		case strings.Contains(w, "month"):
			return cycleResetMonthly, true
		case strings.Contains(w, "week"), w == "7d":
			return cycleResetWeekly, true
		}
	}

	switch {
	case isCycleResetMonthlyKey(k):
		return cycleResetMonthly, true
	case isCycleResetWeeklyKey(k):
		return cycleResetWeekly, true
	}
	return 0, false
}

func isCycleResetMonthlyKey(k string) bool {
	switch k {
	case "billing_cycle_end", "billing_period", "plan_percent_used", "cursor_plan_usage",
		"monthly_subscription", "monthly_credits", "monthly_usage_pct", "monthly_usage":
		return true
	}
	return strings.Contains(k, "monthly")
}

func isCycleResetWeeklyKey(k string) bool {
	switch k {
	case "weekly_usage", "usage_seven_day":
		return true
	}
	return strings.Contains(k, "_weekly") || strings.Contains(k, "_7d") || strings.Contains(k, "seven_day")
}

func collectCycleResetEntries(snap core.UsageSnapshot) []cycleResetEntry {
	if len(snap.Resets) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var entries []cycleResetEntry
	for key, at := range snap.Resets {
		if at.IsZero() || time.Until(at) < 0 {
			continue
		}
		tier, ok := cycleResetTierForKey(key, snap)
		if !ok {
			continue
		}
		dayKey := fmt.Sprintf("%d:%d:%d:%d", tier, at.Year(), at.Month(), at.Day())
		if seen[dayKey] {
			continue
		}
		seen[dayKey] = true
		entries = append(entries, cycleResetEntry{tier: tier, at: at})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].tier != entries[j].tier {
			return entries[i].tier < entries[j].tier
		}
		return entries[i].at.Before(entries[j].at)
	})
	return entries
}

// formatCycleResetDuration renders time-until-reset. Uses whole days when 2+ days
// remain; switches to hours/minutes when under 2 days.
func formatCycleResetDuration(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	if d >= 2*24*time.Hour {
		days := int((d + 12*time.Hour) / (24 * time.Hour))
		if days == 1 {
			return "1 day"
		}
		return fmt.Sprintf("%d days", days)
	}
	return formatDurationShort(d)
}

func formatCycleResetIn(at time.Time, now time.Time) string {
	d := at.Sub(now)
	if dur := formatCycleResetDuration(d); dur != "" {
		return "Resets in " + dur
	}
	return ""
}

func formatCycleResetTierLabel(tier cycleResetTier, dates []time.Time, now time.Time) string {
	if len(dates) == 0 {
		return ""
	}
	prefix := "Monthly resets in"
	if tier == cycleResetWeekly {
		prefix = "Weekly resets in"
	}

	parts := make([]string, 0, len(dates))
	for _, at := range dates {
		if dur := formatCycleResetDuration(at.Sub(now)); dur != "" {
			parts = append(parts, dur)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return prefix + " " + parts[0]
	}
	return prefix + " " + strings.Join(parts, " · ")
}

func formatCycleResetSchedule(snap core.UsageSnapshot, now time.Time) string {
	entries := collectCycleResetEntries(snap)
	if len(entries) == 0 {
		return ""
	}

	if len(entries) == 1 {
		return formatCycleResetIn(entries[0].at, now)
	}

	var monthlyDates, weeklyDates []time.Time
	for _, e := range entries {
		switch e.tier {
		case cycleResetMonthly:
			monthlyDates = append(monthlyDates, e.at)
		case cycleResetWeekly:
			weeklyDates = append(weeklyDates, e.at)
		}
	}

	var parts []string
	if line := formatCycleResetTierLabel(cycleResetMonthly, monthlyDates, now); line != "" {
		parts = append(parts, line)
	}
	if line := formatCycleResetTierLabel(cycleResetWeekly, weeklyDates, now); line != "" {
		parts = append(parts, line)
	}
	return strings.Join(parts, " · ")
}

// formatCycleResetScheduleSidebar renders the primary cycle reset for the navigator row.
func formatCycleResetScheduleSidebar(snap core.UsageSnapshot, now time.Time) string {
	entries := collectCycleResetEntries(snap)
	if len(entries) == 0 {
		return ""
	}
	return formatCycleResetIn(entries[0].at, now)
}

// formatCycleResetScheduleCompact renders all cycle resets on one line.
func formatCycleResetScheduleCompact(snap core.UsageSnapshot, now time.Time) string {
	entries := collectCycleResetEntries(snap)
	if len(entries) == 0 {
		return ""
	}

	parts := make([]string, 0, len(entries))
	for _, e := range entries {
		if dur := formatCycleResetDuration(e.at.Sub(now)); dur != "" {
			parts = append(parts, dur)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Resets in " + strings.Join(parts, " · ")
}

func formatLastRefreshedIfStale(timestamp time.Time, now time.Time) string {
	if timestamp.IsZero() {
		return ""
	}
	if now.Sub(timestamp) <= 5*time.Minute {
		return ""
	}
	return formatLastRefreshed(timestamp, now)
}
