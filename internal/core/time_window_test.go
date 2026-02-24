package core

import "testing"

func TestTimeWindowHours(t *testing.T) {
	tests := []struct {
		tw   TimeWindow
		want int
	}{
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

func TestNextTimeWindow(t *testing.T) {
	tests := []struct {
		current TimeWindow
		want    TimeWindow
	}{
		{TimeWindow1d, TimeWindow3d},
		{TimeWindow3d, TimeWindow7d},
		{TimeWindow7d, TimeWindow30d},
		{TimeWindow30d, TimeWindow1d},
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
