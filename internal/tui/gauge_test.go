package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestRenderStackedUsageGauge_TwoSegments(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 30, Color: lipgloss.Color("#00ff00")},
		{Percent: 20, Color: lipgloss.Color("#ffaa00")},
	}
	out := RenderStackedUsageGauge(segments, 50, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "50.0%") {
		t.Fatalf("output should contain '50.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_ZeroPercent(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 0, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, 0, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "0.0%") {
		t.Fatalf("output should contain '0.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_HundredPercent(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 60, Color: lipgloss.Color("#ff0000")},
		{Percent: 40, Color: lipgloss.Color("#0000ff")},
	}
	out := RenderStackedUsageGauge(segments, 100, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "100.0%") {
		t.Fatalf("output should contain '100.0%%', got %q", out)
	}
	// At 100%, the track character should not appear.
	if strings.Contains(out, "░") {
		t.Fatal("100% gauge should not contain empty track characters")
	}
}

func TestRenderStackedUsageGauge_SingleSegment(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 75, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, 75, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "75.0%") {
		t.Fatalf("output should contain '75.0%%', got %q", out)
	}
}

func TestRenderStackedUsageGauge_NegativeRendersNA(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 50, Color: lipgloss.Color("#00ff00")},
	}
	out := RenderStackedUsageGauge(segments, -1, 20)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "N/A") {
		t.Fatalf("negative totalPercent should render N/A, got %q", out)
	}
}

func TestRenderShimmerGauge(t *testing.T) {
	out := RenderShimmerGauge(20, 0)
	if out == "" {
		t.Fatal("expected non-empty output")
	}
	if !strings.Contains(out, "···") {
		t.Fatalf("shimmer gauge should contain loading indicator, got %q", out)
	}
	// Verify it renders at different frames without panic.
	for f := 0; f < 30; f++ {
		if RenderShimmerGauge(20, f) == "" {
			t.Fatalf("empty output at frame %d", f)
		}
	}
}

func TestRenderShimmerGauge_NarrowWidth(t *testing.T) {
	out := RenderShimmerGauge(2, 0)
	if out == "" {
		t.Fatal("expected non-empty output for narrow width")
	}
}

func TestRenderStackedUsageGauge_NarrowWidth(t *testing.T) {
	segments := []GaugeSegment{
		{Percent: 30, Color: lipgloss.Color("#00ff00")},
		{Percent: 20, Color: lipgloss.Color("#ffaa00")},
	}
	out := RenderStackedUsageGauge(segments, 50, 2)
	if out == "" {
		t.Fatal("expected non-empty output for narrow width")
	}
	if !strings.Contains(out, "50.0%") {
		t.Fatalf("narrow width output should still contain '50.0%%', got %q", out)
	}
}
