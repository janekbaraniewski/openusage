package shared

import (
	"strings"
	"testing"
)

func TestNormalizeLooseModelName(t *testing.T) {
	if got := NormalizeLooseModelName("  claude-sonnet-4  "); got != "claude-sonnet-4" {
		t.Fatalf("NormalizeLooseModelName(trimmed) = %q", got)
	}
	if got := NormalizeLooseModelName("   "); got != "unknown" {
		t.Fatalf("NormalizeLooseModelName(empty) = %q", got)
	}
}

func TestNormalizeLooseClientName(t *testing.T) {
	if got := NormalizeLooseClientName("  CLI  "); got != "CLI" {
		t.Fatalf("NormalizeLooseClientName(trimmed) = %q", got)
	}
	if got := NormalizeLooseClientName(""); got != "Other" {
		t.Fatalf("NormalizeLooseClientName(empty) = %q", got)
	}
}

func TestSanitizeMetricName(t *testing.T) {
	got := SanitizeMetricName(" GPT-4.1 / Mini ")
	if got != "gpt_4_1_mini" {
		t.Fatalf("SanitizeMetricName() = %q", got)
	}
}

func TestSummarizeShareUsage(t *testing.T) {
	got := SummarizeShareUsage(map[string]float64{
		"beta":  25,
		"alpha": 75,
		"zero":  0,
	}, 2, func(name string) string { return strings.ToUpper(name) })
	want := "ALPHA: 75%, BETA: 25%"
	if got != want {
		t.Fatalf("SummarizeShareUsage() = %q, want %q", got, want)
	}
}

func TestSummarizeCountUsage(t *testing.T) {
	got := SummarizeCountUsage(map[string]float64{
		"beta":  2,
		"alpha": 3,
	}, "req", 2, func(name string) string { return strings.ToUpper(name) })
	want := "ALPHA: 3 req, BETA: 2 req"
	if got != want {
		t.Fatalf("SummarizeCountUsage() = %q, want %q", got, want)
	}
}
