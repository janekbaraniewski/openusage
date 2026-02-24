package core

// TimeWindow represents a configurable time window for filtering usage data.
type TimeWindow string

const (
	TimeWindow1d  TimeWindow = "1d"
	TimeWindow3d  TimeWindow = "3d"
	TimeWindow7d  TimeWindow = "7d"
	TimeWindow30d TimeWindow = "30d"
)

var ValidTimeWindows = []TimeWindow{
	TimeWindow1d,
	TimeWindow3d,
	TimeWindow7d,
	TimeWindow30d,
}

// Hours returns the window size in hours.
func (tw TimeWindow) Hours() int {
	switch tw {
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
// (e.g., "-7 day").
func (tw TimeWindow) SQLiteOffset() string {
	switch tw {
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

func ParseTimeWindow(s string) TimeWindow {
	for _, tw := range ValidTimeWindows {
		if string(tw) == s {
			return tw
		}
	}
	return TimeWindow30d
}

// LargestWindowFitting returns the largest valid TimeWindow whose Days() <= maxDays.
// Falls back to the smallest window if none fit.
func LargestWindowFitting(maxDays int) TimeWindow {
	var best TimeWindow
	for _, tw := range ValidTimeWindows {
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
