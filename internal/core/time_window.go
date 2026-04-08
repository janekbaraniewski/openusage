package core

import "time"

// TimeWindow represents a configurable time window for filtering usage data.
type TimeWindow string

const (
	TimeWindow1d  TimeWindow = "1d"
	TimeWindow3d  TimeWindow = "3d"
	TimeWindow7d  TimeWindow = "7d"
	TimeWindow30d TimeWindow = "30d"
	TimeWindowAll TimeWindow = "all"
)

var ValidTimeWindows = []TimeWindow{
	TimeWindow1d,
	TimeWindow3d,
	TimeWindow7d,
	TimeWindow30d,
	TimeWindowAll,
}

// Hours returns the window size in hours. Returns 0 for TimeWindowAll (no filter).
func (tw TimeWindow) Hours() int {
	switch tw {
	case TimeWindowAll:
		return 0
	case TimeWindow1d:
		return 24
	case TimeWindow3d:
		return 3 * 24
	case TimeWindow7d:
		return 7 * 24
	case TimeWindow30d:
		return 30 * 24
	default:
		return 30 * 24
	}
}

// Days returns the window size in days.
func (tw TimeWindow) Days() int {
	return tw.Hours() / 24
}

func (tw TimeWindow) Label() string {
	switch tw {
	case TimeWindowAll:
		return "All Time"
	case TimeWindow1d:
		return "Today"
	case TimeWindow3d:
		return "3 Days"
	case TimeWindow7d:
		return "7 Days"
	case TimeWindow30d:
		return "30 Days"
	default:
		return "30 Days"
	}
}

// SQLiteOffset returns the SQLite datetime offset string for this window
// (e.g., "-7 day"). Returns empty string for TimeWindowAll (no filter).
func (tw TimeWindow) SQLiteOffset() string {
	switch tw {
	case TimeWindowAll:
		return ""
	case TimeWindow1d:
		return "-1 day"
	case TimeWindow3d:
		return "-3 day"
	case TimeWindow7d:
		return "-7 day"
	case TimeWindow30d:
		return "-30 day"
	default:
		return "-30 day"
	}
}

// LocalMidnight returns midnight (00:00:00) of the current local day.
func LocalMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
}

// Since returns the cutoff time for this window.
// For "1d" (Today): local midnight (calendar day boundary).
// For "3d", "7d", "30d": rolling N*24 hours from now.
// For "all": zero time (no filter).
func (tw TimeWindow) Since() time.Time {
	now := time.Now()
	switch tw {
	case TimeWindowAll:
		return time.Time{}
	case TimeWindow1d:
		return LocalMidnight()
	case TimeWindow3d:
		return now.Add(-3 * 24 * time.Hour)
	case TimeWindow7d:
		return now.Add(-7 * 24 * time.Hour)
	case TimeWindow30d:
		return now.Add(-30 * 24 * time.Hour)
	default:
		return now.Add(-30 * 24 * time.Hour)
	}
}

func ParseTimeWindow(s string) TimeWindow {
	for _, tw := range ValidTimeWindows {
		if string(tw) == s {
			return tw
		}
	}
	return TimeWindow30d
}

// LargestWindowFitting returns the largest valid TimeWindow whose Days() <= maxDays.
// Falls back to the smallest window if none fit. Skips TimeWindowAll.
func LargestWindowFitting(maxDays int) TimeWindow {
	var best TimeWindow
	for _, tw := range ValidTimeWindows {
		if tw == TimeWindowAll {
			continue
		}
		if tw.Days() <= maxDays {
			if best == "" || tw.Days() > best.Days() {
				best = tw
			}
		}
	}
	if best == "" {
		return ValidTimeWindows[0]
	}
	return best
}

// NextTimeWindow returns the next time window in the cycle.
func NextTimeWindow(current TimeWindow) TimeWindow {
	for i, tw := range ValidTimeWindows {
		if tw == current {
			return ValidTimeWindows[(i+1)%len(ValidTimeWindows)]
		}
	}
	return ValidTimeWindows[0]
}
