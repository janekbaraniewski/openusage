package active

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		in   time.Duration
		want string
	}{
		{2*24*time.Hour + 30*time.Second, "2d 0h"},
		{6*24*time.Hour + 21*time.Hour, "6d 21h"},
		{2 * time.Hour, "2h"},
		{2*time.Hour + 14*time.Minute, "2h 14m"},
		{90 * time.Second, "2m"},
		{1 * time.Second, "1m"},
		{0, "1m"},
	}
	for _, tc := range tests {
		if got := FormatDuration(tc.in); got != tc.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNarrateLabelGrammar(t *testing.T) {
	now := at("2026-08-15T12:00:00Z")
	pct := func(v float64) *float64 { return &v }
	ts := func(s string) *time.Time { v := at(s); return &v }

	tests := []struct {
		name         string
		facts        Facts
		wantLabel    string
		wantSeverity Severity
	}{
		{
			name: "runout and reset known",
			facts: Facts{
				RunoutAt:          ts("2026-08-17T12:00:00Z"),
				ResetAt:           ts("2026-08-22T09:00:00Z"),
				RunoutBeforeReset: true,
				PctRemaining:      pct(40),
			},
			wantLabel: "2d 0h/6d 21h", wantSeverity: SeverityWarn,
		},
		{
			name: "at cap with reset",
			facts: Facts{
				AtCap:   true,
				ResetAt: ts("2026-08-22T09:00:00Z"),
			},
			wantLabel: "cap/6d 21h", wantSeverity: SeverityBad,
		},
		{
			name: "runout without reset",
			facts: Facts{
				RunoutAt:          ts("2026-08-17T12:00:00Z"),
				RunoutBeforeReset: true,
				PctRemaining:      pct(40),
			},
			wantLabel: "out ~2d 0h", wantSeverity: SeverityWarn,
		},
		{
			name: "no runout, quota and reset",
			facts: Facts{
				PctRemaining: pct(37),
				ResetAt:      ts("2026-08-15T14:00:00Z"),
			},
			wantLabel: "37% left/reset 2h", wantSeverity: SeverityGood,
		},
		{
			name:         "no runout, quota only",
			facts:        Facts{PctRemaining: pct(37)},
			wantLabel:    "37% left",
			wantSeverity: SeverityGood,
		},
		{
			name:         "no quota at all",
			facts:        Facts{RequestsToday: pct(1204)},
			wantLabel:    "1,204 req today",
			wantSeverity: SeverityWarn,
		},
		{
			name: "runout after reset is suppressed",
			facts: Facts{
				RunoutAt:          ts("2026-08-25T12:00:00Z"),
				ResetAt:           ts("2026-08-15T14:00:00Z"),
				RunoutBeforeReset: false,
				PctRemaining:      pct(37),
			},
			wantLabel: "37% left/reset 2h", wantSeverity: SeverityGood,
		},
		{
			name:         "low remaining is bad",
			facts:        Facts{PctRemaining: pct(8)},
			wantLabel:    "8% left",
			wantSeverity: SeverityBad,
		},
		{
			name:         "medium remaining is warn",
			facts:        Facts{PctRemaining: pct(20)},
			wantLabel:    "20% left",
			wantSeverity: SeverityWarn,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			label, severity := Narrate(tc.facts, now)
			if label != tc.wantLabel {
				t.Errorf("label = %q, want %q", label, tc.wantLabel)
			}
			if severity != tc.wantSeverity {
				t.Errorf("severity = %q, want %q", severity, tc.wantSeverity)
			}
		})
	}
}
