package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestRenderSparklineNtcharts(t *testing.T) {
	out := RenderSparkline([]float64{1, 3, 2, 5, 4, 8}, 6, colorTeal)
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected sparkline output")
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("expected single-line sparkline, got %q", out)
	}
}

func TestRenderBrailleChartNtcharts(t *testing.T) {
	out := RenderBrailleChart("Daily Cost", []BrailleSeries{
		{
			Label: "cost",
			Color: colorTeal,
			Points: []core.TimePoint{
				{Date: "2026-04-01", Value: 10},
				{Date: "2026-04-02", Value: 15},
				{Date: "2026-04-03", Value: 7},
			},
		},
	}, 60, 8, formatCostAxis)
	if !strings.Contains(out, "Daily Cost") {
		t.Fatalf("expected chart title, got:\n%s", out)
	}
	if !strings.Contains(out, "cost") {
		t.Fatalf("expected legend label, got:\n%s", out)
	}
}

func TestRenderTimeChartStackedNtcharts(t *testing.T) {
	out := RenderTimeChart(TimeChartSpec{
		Title:  "Provider Burn",
		Mode:   TimeChartStacked,
		Height: 8,
		Series: []BrailleSeries{
			{
				Label: "OpenAI",
				Color: colorTeal,
				Points: []core.TimePoint{
					{Date: "2026-04-01", Value: 10},
					{Date: "2026-04-02", Value: 15},
					{Date: "2026-04-03", Value: 20},
				},
			},
			{
				Label: "Anthropic",
				Color: colorPeach,
				Points: []core.TimePoint{
					{Date: "2026-04-01", Value: 5},
					{Date: "2026-04-02", Value: 3},
					{Date: "2026-04-03", Value: 9},
				},
			},
		},
	}, 72)
	if !strings.Contains(out, "Provider Burn") {
		t.Fatalf("expected chart title, got:\n%s", out)
	}
	if !strings.Contains(out, "OpenAI") || !strings.Contains(out, "Anthropic") {
		t.Fatalf("expected series legend labels, got:\n%s", out)
	}
}

func TestRenderHeatmapNtcharts(t *testing.T) {
	out := RenderHeatmap(HeatmapSpec{
		Title: "Model Heat",
		Rows:  []string{"GPT-5", "Claude"},
		Cols:  []string{"2026-04-01", "2026-04-02", "2026-04-03"},
		Values: [][]float64{
			{1, 5, 2},
			{0, 3, 8},
		},
		RowColors: []lipgloss.Color{colorTeal, colorPeach},
	}, 60)
	if !strings.Contains(out, "Model Heat") {
		t.Fatalf("expected heatmap title, got:\n%s", out)
	}
	if !strings.Contains(out, "GPT-5") || !strings.Contains(out, "Claude") {
		t.Fatalf("expected row labels, got:\n%s", out)
	}
}

func TestRenderToolMixBarNtcharts(t *testing.T) {
	colors := map[string]lipgloss.Color{
		"read": colorTeal,
		"edit": colorPeach,
	}
	out := renderToolMixBar([]toolMixEntry{
		{name: "read", count: 7},
		{name: "edit", count: 3},
	}, 10, 20, colors)
	if out == "" {
		t.Fatal("expected stacked tool bar output")
	}
	if got := len(stripANSI(out)); got != 20 {
		t.Fatalf("expected visible width 20, got %d", got)
	}
}

func TestSanitizeSeriesPoints_ClampsNegatives(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-01-01", Value: 100},
		{Date: "2026-01-02", Value: -41.36},
		{Date: "2026-01-03", Value: 200},
	}
	sanitized := sanitizeSeriesPoints(pts)
	if sanitized[1].Value != 0 {
		t.Errorf("expected 0, got %f", sanitized[1].Value)
	}
	// Original unchanged.
	if pts[1].Value != -41.36 {
		t.Error("original modified")
	}
}

func TestSanitizeSeriesPoints_PreservesPositives(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-01-01", Value: 50},
		{Date: "2026-01-02", Value: 0},
		{Date: "2026-01-03", Value: 300},
	}
	sanitized := sanitizeSeriesPoints(pts)
	for i, p := range sanitized {
		if p.Value != pts[i].Value {
			t.Errorf("index %d: expected %f, got %f", i, pts[i].Value, p.Value)
		}
	}
}

func TestTrimLeadingTrailingZeros(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-01-01", Value: 0},
		{Date: "2026-01-02", Value: 0},
		{Date: "2026-01-03", Value: 100},
		{Date: "2026-01-04", Value: 200},
		{Date: "2026-01-05", Value: 0},
		{Date: "2026-01-06", Value: 0},
	}
	trimmed := trimLeadingTrailingZeros(pts)
	// Should keep one zero on each side: [Jan 02, Jan 03, Jan 04, Jan 05]
	if len(trimmed) != 4 {
		t.Fatalf("expected 4 points, got %d", len(trimmed))
	}
	if trimmed[0].Date != "2026-01-02" {
		t.Errorf("expected start at Jan 02, got %s", trimmed[0].Date)
	}
	if trimmed[3].Date != "2026-01-05" {
		t.Errorf("expected end at Jan 05, got %s", trimmed[3].Date)
	}
}

func TestTrimLeadingTrailingZeros_AllNonZero(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-01-01", Value: 10},
		{Date: "2026-01-02", Value: 20},
	}
	trimmed := trimLeadingTrailingZeros(pts)
	if len(trimmed) != 2 {
		t.Fatalf("expected 2 points unchanged, got %d", len(trimmed))
	}
}

func TestChartSeriesBounds_FloorsAtZero(t *testing.T) {
	series := []BrailleSeries{{
		Label: "test",
		Color: colorTeal,
		Points: []core.TimePoint{
			{Date: "2026-01-01", Value: -50},
			{Date: "2026-01-02", Value: 300},
		},
	}}
	_, _, minY, _, ok := chartSeriesBounds(series)
	if !ok {
		t.Fatal("expected ok")
	}
	if minY < 0 {
		t.Errorf("minY should be >= 0, got %f", minY)
	}
}

func TestBinSeriesValues_SumsNotAverages(t *testing.T) {
	dates := []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04"}
	values := [][]float64{{700, 0, 0, 0}}
	_, binned := binSeriesValues(dates, values, 2)
	// First bin covers dates 0-1: 700+0 = 700 (not 350)
	if binned[0][0] != 700 {
		t.Errorf("expected sum 700, got %f", binned[0][0])
	}
	// Second bin covers dates 2-3: 0+0 = 0
	if binned[0][1] != 0 {
		t.Errorf("expected 0, got %f", binned[0][1])
	}
}

func TestRenderBrailleChart_NegativeValuesClampedToZero(t *testing.T) {
	out := RenderBrailleChart("Test", []BrailleSeries{
		{
			Label: "cost",
			Color: colorTeal,
			Points: []core.TimePoint{
				{Date: "2026-04-01", Value: 100},
				{Date: "2026-04-02", Value: -41},
				{Date: "2026-04-03", Value: 200},
			},
		},
	}, 60, 8, formatCostAxis)
	// Should not contain negative values in Y-axis labels.
	if strings.Contains(out, "-$") || strings.Contains(out, "$-") {
		t.Errorf("chart should not show negative cost values:\n%s", out)
	}
}

func TestFillSeriesDateGaps(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-04-01", Value: 100},
		{Date: "2026-04-03", Value: 200},
		{Date: "2026-04-06", Value: 50},
	}
	filled := fillSeriesDateGaps(pts)
	// Should have 6 days: Apr 1-6.
	if len(filled) != 6 {
		t.Fatalf("expected 6 points, got %d", len(filled))
	}
	// Apr 2 should be 0 (gap day).
	if filled[1].Date != "2026-04-02" || filled[1].Value != 0 {
		t.Errorf("expected Apr 02 = 0, got %s = %f", filled[1].Date, filled[1].Value)
	}
	// Apr 4 and 5 should be 0 (gap days).
	if filled[3].Value != 0 {
		t.Errorf("expected Apr 04 = 0, got %f", filled[3].Value)
	}
	if filled[4].Value != 0 {
		t.Errorf("expected Apr 05 = 0, got %f", filled[4].Value)
	}
	// Original values preserved.
	if filled[0].Value != 100 || filled[2].Value != 200 || filled[5].Value != 50 {
		t.Error("original values not preserved")
	}
}

func TestFillSeriesDateGaps_NoGaps(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-04-01", Value: 10},
		{Date: "2026-04-02", Value: 20},
		{Date: "2026-04-03", Value: 30},
	}
	filled := fillSeriesDateGaps(pts)
	if len(filled) != 3 {
		t.Fatalf("no gaps, expected 3 points, got %d", len(filled))
	}
}

func TestClipAndPadPointsByRecentDays_FillsRequestedWindow(t *testing.T) {
	pts := []core.TimePoint{
		{Date: "2026-04-01", Value: 100},
		{Date: "2026-04-03", Value: 200},
	}

	got := clipAndPadPointsByRecentDays(pts, 5, time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC))
	if len(got) != 5 {
		t.Fatalf("expected 5 points, got %d", len(got))
	}
	if got[0].Date != "2026-04-01" || got[4].Date != "2026-04-05" {
		t.Fatalf("unexpected padded range: %s..%s", got[0].Date, got[4].Date)
	}
	if got[1].Value != 0 || got[3].Value != 0 || got[4].Value != 0 {
		t.Fatalf("expected zero padding on missing days, got %+v", got)
	}
}

func TestRenderNTStackedBarUsesRequestedWidth(t *testing.T) {
	out := renderNTStackedBar([]ntBarSegment{
		{Value: 6, Color: colorTeal},
		{Value: 3, Color: colorPeach},
		{Value: 1, Color: colorYellow},
	}, 10, 24)

	if out == "" {
		t.Fatal("expected stacked bar output")
	}
	if strings.Contains(out, "\n") {
		t.Fatalf("expected single-line output, got %q", out)
	}
	if got := len(stripANSI(out)); got != 24 {
		t.Fatalf("expected visible width 24, got %d (%q)", got, stripANSI(out))
	}
}
