package core

import (
	"testing"
	"time"
)

func TestTimeWindowHours(t *testing.T) {
	tests := []struct {
		tw   TimeWindow
		want int
	}{
		{TimeWindowAll, 0},
		{TimeWindow1d, 24},
		{TimeWindow3d, 72},
		{TimeWindow7d, 168},
		{TimeWindow30d, 720},
		{TimeWindow(""), 720},
		{TimeWindow("999d"), 720},
	}
	for _, tt := range tests {
		t.Run(string(tt.tw), func(t *testing.T) {
			if got := tt.tw.Hours(); got != tt.want {
				t.Errorf("TimeWindow(%q).Hours() = %d, want %d", tt.tw, got, tt.want)
			}
		})
	}
}

func TestTimeWindowDays(t *testing.T) {
	tests := []struct {
		tw   TimeWindow
		want int
	}{
		{TimeWindow1d, 1},
		{TimeWindow3d, 3},
		{TimeWindow7d, 7},
		{TimeWindow30d, 30},
		{TimeWindow(""), 30},
		{TimeWindow("999d"), 30},
	}
	for _, tt := range tests {
		t.Run(string(tt.tw), func(t *testing.T) {
			if got := tt.tw.Days(); got != tt.want {
				t.Errorf("TimeWindow(%q).Days() = %d, want %d", tt.tw, got, tt.want)
			}
		})
	}
}

func TestTimeWindowLabel(t *testing.T) {
	tests := []struct {
		tw   TimeWindow
		want string
	}{
		{TimeWindowAll, "All Time"},
		{TimeWindow1d, "Today"},
		{TimeWindow3d, "3 Days"},
		{TimeWindow7d, "7 Days"},
		{TimeWindow30d, "30 Days"},
		{TimeWindow(""), "30 Days"},
		{TimeWindow("unknown"), "30 Days"},
	}
	for _, tt := range tests {
		t.Run(string(tt.tw), func(t *testing.T) {
			if got := tt.tw.Label(); got != tt.want {
				t.Errorf("TimeWindow(%q).Label() = %q, want %q", tt.tw, got, tt.want)
			}
		})
	}
}

func TestTimeWindowSQLiteOffset(t *testing.T) {
	tests := []struct {
		tw   TimeWindow
		want string
	}{
		{TimeWindowAll, ""},
		{TimeWindow1d, "-1 day"},
		{TimeWindow3d, "-3 day"},
		{TimeWindow7d, "-7 day"},
		{TimeWindow30d, "-30 day"},
		{TimeWindow(""), "-30 day"},
	}
	for _, tt := range tests {
		t.Run(string(tt.tw), func(t *testing.T) {
			if got := tt.tw.SQLiteOffset(); got != tt.want {
				t.Errorf("TimeWindow(%q).SQLiteOffset() = %q, want %q", tt.tw, got, tt.want)
			}
		})
	}
}

func TestParseTimeWindow(t *testing.T) {
	tests := []struct {
		input string
		want  TimeWindow
	}{
		{"all", TimeWindowAll},
		{"1d", TimeWindow1d},
		{"3d", TimeWindow3d},
		{"7d", TimeWindow7d},
		{"30d", TimeWindow30d},
		{"", TimeWindow30d},
		{"bogus", TimeWindow30d},
		{"14d", TimeWindow30d},
		{"1h", TimeWindow30d},
		{"12h", TimeWindow30d},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			if got := ParseTimeWindow(tt.input); got != tt.want {
				t.Errorf("ParseTimeWindow(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestLargestWindowFitting(t *testing.T) {
	tests := []struct {
		maxDays int
		want    TimeWindow
	}{
		{0, TimeWindow1d},
		{1, TimeWindow1d},
		{2, TimeWindow1d},
		{3, TimeWindow3d},
		{6, TimeWindow3d},
		{7, TimeWindow7d},
		{10, TimeWindow7d},
		{29, TimeWindow7d},
		{30, TimeWindow30d},
		{90, TimeWindow30d},
	}
	for _, tt := range tests {
		if got := LargestWindowFitting(tt.maxDays); got != tt.want {
			t.Errorf("LargestWindowFitting(%d) = %q, want %q", tt.maxDays, got, tt.want)
		}
	}
}

func TestLocalMidnight(t *testing.T) {
	m := LocalMidnight()
	if m.Hour() != 0 || m.Minute() != 0 || m.Second() != 0 || m.Nanosecond() != 0 {
		t.Errorf("LocalMidnight() = %v, want 00:00:00.000000000", m)
	}
	now := time.Now()
	if m.Year() != now.Year() || m.Month() != now.Month() || m.Day() != now.Day() {
		t.Errorf("LocalMidnight() date = %v, want %v", m.Format("2006-01-02"), now.Format("2006-01-02"))
	}
	if m.Location() != now.Location() {
		t.Errorf("LocalMidnight() location = %v, want %v", m.Location(), now.Location())
	}
}

func TestTimeWindowSince(t *testing.T) {
	now := time.Now()

	// "all" returns zero time.
	allSince := TimeWindowAll.Since()
	if !allSince.IsZero() {
		t.Errorf("TimeWindowAll.Since() = %v, want zero", allSince)
	}

	// "1d" returns local midnight (calendar day boundary).
	oneDaySince := TimeWindow1d.Since()
	if oneDaySince.Hour() != 0 || oneDaySince.Minute() != 0 || oneDaySince.Second() != 0 {
		t.Errorf("TimeWindow1d.Since() = %v, want midnight", oneDaySince)
	}
	if oneDaySince.Year() != now.Year() || oneDaySince.Month() != now.Month() || oneDaySince.Day() != now.Day() {
		t.Errorf("TimeWindow1d.Since() date = %v, want today", oneDaySince.Format("2006-01-02"))
	}

	// "3d" returns ~72h ago (rolling).
	threeDaySince := TimeWindow3d.Since()
	diff3d := now.Sub(threeDaySince)
	if diff3d < 71*time.Hour || diff3d > 73*time.Hour {
		t.Errorf("TimeWindow3d.Since() diff = %v, want ~72h", diff3d)
	}

	// "7d" returns ~168h ago (rolling).
	sevenDaySince := TimeWindow7d.Since()
	diff7d := now.Sub(sevenDaySince)
	if diff7d < 167*time.Hour || diff7d > 169*time.Hour {
		t.Errorf("TimeWindow7d.Since() diff = %v, want ~168h", diff7d)
	}

	// "30d" returns ~720h ago (rolling).
	thirtyDaySince := TimeWindow30d.Since()
	diff30d := now.Sub(thirtyDaySince)
	if diff30d < 719*time.Hour || diff30d > 721*time.Hour {
		t.Errorf("TimeWindow30d.Since() diff = %v, want ~720h", diff30d)
	}

	// Unknown defaults to 30d.
	unknownSince := TimeWindow("bogus").Since()
	diffUnknown := now.Sub(unknownSince)
	if diffUnknown < 719*time.Hour || diffUnknown > 721*time.Hour {
		t.Errorf("TimeWindow(bogus).Since() diff = %v, want ~720h", diffUnknown)
	}
}

func TestNextTimeWindow(t *testing.T) {
	tests := []struct {
		current TimeWindow
		want    TimeWindow
	}{
		{TimeWindow1d, TimeWindow3d},
		{TimeWindow3d, TimeWindow7d},
		{TimeWindow7d, TimeWindow30d},
		{TimeWindow30d, TimeWindowAll},
		{TimeWindowAll, TimeWindow1d},
		{TimeWindow("unknown"), TimeWindow1d},
	}
	for _, tt := range tests {
		t.Run(string(tt.current), func(t *testing.T) {
			if got := NextTimeWindow(tt.current); got != tt.want {
				t.Errorf("NextTimeWindow(%q) = %q, want %q", tt.current, got, tt.want)
			}
		})
	}
}
