package active

import (
	"fmt"
	"strings"
	"time"
)

// FormatDuration renders a countdown rounded up to the nearest minute. Days
// always include their hours component so the field width stays stable.
func FormatDuration(d time.Duration) string {
	minutes := int((d + 59*time.Second) / time.Minute)
	if minutes < 1 {
		minutes = 1
	}
	days := minutes / 1440
	minutes -= days * 1440
	hours := minutes / 60
	minutes -= hours * 60

	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, hours)
	case hours > 0:
		if minutes > 0 {
			return fmt.Sprintf("%dh %dm", hours, minutes)
		}
		return fmt.Sprintf("%dh", hours)
	default:
		return fmt.Sprintf("%dm", minutes)
	}
}

func thousands(n int) string {
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	lead := len(s) % 3
	if lead > 0 {
		b.WriteString(s[:lead])
	}
	for i := lead; i < len(s); i += 3 {
		if b.Len() > 0 {
			b.WriteByte(',')
		}
		b.WriteString(s[i : i+3])
	}
	return b.String()
}

// Narrate renders facts into a compact status label and severity band.
func Narrate(f Facts, now time.Time) (string, Severity) {
	severity := severityFor(f)

	var resetLabel string
	if f.ResetAt != nil {
		if d := f.ResetAt.Sub(now); d > 0 {
			resetLabel = FormatDuration(d)
		} else {
			resetLabel = "now"
		}
	}

	if f.AtCap {
		if resetLabel != "" {
			return "cap/" + resetLabel, severity
		}
		return "at cap", severity
	}

	if f.RunoutAt != nil && f.RunoutBeforeReset {
		runout := FormatDuration(f.RunoutAt.Sub(now))
		if resetLabel != "" {
			return runout + "/" + resetLabel, severity
		}
		return "out ~" + runout, severity
	}

	if f.PctRemaining != nil {
		pct := fmt.Sprintf("%.0f%% left", *f.PctRemaining)
		if resetLabel != "" {
			return pct + "/reset " + resetLabel, severity
		}
		return pct, severity
	}

	if f.RequestsToday != nil {
		return thousands(int(*f.RequestsToday)) + " req today", SeverityWarn
	}
	return "quota unavailable", SeverityWarn
}

func severityFor(f Facts) Severity {
	if f.AtCap {
		return SeverityBad
	}
	if f.PctRemaining == nil {
		return SeverityWarn
	}
	switch {
	case *f.PctRemaining <= 10:
		return SeverityBad
	case f.RunoutAt != nil && f.RunoutBeforeReset:
		// A forecasted exhaustion before the reset is actionable even when the
		// instantaneous remaining percentage still looks healthy.
		return SeverityWarn
	case *f.PctRemaining <= 25:
		return SeverityWarn
	default:
		return SeverityGood
	}
}
