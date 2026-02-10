package parsers

import (
	"net/http"
	"testing"
	"time"
)

func float64Ptr(v float64) *float64 { return &v }

func TestParseFloat(t *testing.T) {
	tests := []struct {
		input string
		want  *float64
	}{
		{"100", float64Ptr(100)},
		{"3.14", float64Ptr(3.14)},
		{"", nil},
		{"abc", nil},
		{" 42 ", float64Ptr(42)},
	}

	for _, tt := range tests {
		got := ParseFloat(tt.input)
		if tt.want == nil {
			if got != nil {
				t.Errorf("ParseFloat(%q) = %v, want nil", tt.input, *got)
			}
		} else {
			if got == nil {
				t.Errorf("ParseFloat(%q) = nil, want %v", tt.input, *tt.want)
			} else if *got != *tt.want {
				t.Errorf("ParseFloat(%q) = %v, want %v", tt.input, *got, *tt.want)
			}
		}
	}
}

func TestParseResetTime(t *testing.T) {
	// Unix timestamp.
	ts := ParseResetTime("1700000000")
	if ts == nil {
		t.Fatal("expected non-nil for unix timestamp")
	}
	expected := time.Unix(1700000000, 0)
	if !ts.Equal(expected) {
		t.Errorf("got %v, want %v", ts, expected)
	}

	// RFC3339.
	ts = ParseResetTime("2025-01-01T00:00:00Z")
	if ts == nil {
		t.Fatal("expected non-nil for RFC3339")
	}

	// Duration.
	before := time.Now()
	ts = ParseResetTime("30s")
	if ts == nil {
		t.Fatal("expected non-nil for duration")
	}
	if ts.Before(before.Add(29 * time.Second)) {
		t.Error("duration parse too far in past")
	}

	// Empty.
	ts = ParseResetTime("")
	if ts != nil {
		t.Error("expected nil for empty")
	}
}

func TestRedactHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer sk-1234567890abcdef")
	h.Set("Content-Type", "application/json")
	h.Set("X-RateLimit-Remaining", "42")

	redacted := RedactHeaders(h)

	if redacted["Authorization"] == "Bearer sk-1234567890abcdef" {
		t.Error("Authorization should be redacted")
	}
	if redacted["Content-Type"] != "application/json" {
		t.Error("Content-Type should not be redacted")
	}
	if redacted["X-Ratelimit-Remaining"] != "42" {
		t.Errorf("X-RateLimit-Remaining = %q, want '42'", redacted["X-Ratelimit-Remaining"])
	}
}
