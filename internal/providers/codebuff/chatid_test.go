package codebuff

import (
	"testing"
	"time"
)

func TestParseChatIDTimestamp(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    time.Time
		wantOK  bool
	}{
		{
			name:   "regular date only",
			in:     "2025-12-14",
			want:   time.Date(2025, 12, 14, 0, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "date-time with milliseconds Z",
			in:     "2025-12-14T10-00-00.000Z",
			want:   time.Date(2025, 12, 14, 10, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "date-time with positive offset",
			in:     "2025-12-14T10-30-45.123+02-00",
			want:   time.Date(2025, 12, 14, 8, 30, 45, 123000000, time.UTC),
			wantOK: true,
		},
		{
			name:   "date-time with negative offset",
			in:     "2025-12-14T10-30-45.000-05-00",
			want:   time.Date(2025, 12, 14, 15, 30, 45, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "no fractional seconds",
			in:     "2025-12-14T10-00-00Z",
			want:   time.Date(2025, 12, 14, 10, 0, 0, 0, time.UTC),
			wantOK: true,
		},
		{
			name:   "malformed input",
			in:     "not-a-timestamp",
			wantOK: false,
		},
		{
			name:   "empty",
			in:     "",
			wantOK: false,
		},
		{
			name:   "garbage T",
			in:     "2025-12-14Tnope",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseChatIDTimestamp(tc.in)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v (got %v)", ok, tc.wantOK, got)
			}
			if !ok {
				return
			}
			if !got.Equal(tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestParseChatIDTimestamp_DatePreserved guards against a naive global
// replace('-', ':') that would corrupt the date portion.
func TestParseChatIDTimestamp_DatePreserved(t *testing.T) {
	got, ok := parseChatIDTimestamp("2025-12-14T10-00-00.000Z")
	if !ok {
		t.Fatal("parse failed")
	}
	if got.Year() != 2025 || got.Month() != 12 || got.Day() != 14 {
		t.Errorf("date corrupted: got %v", got)
	}
}
