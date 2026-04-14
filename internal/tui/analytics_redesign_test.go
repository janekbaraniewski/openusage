package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestExtractCostDataAggregatesCrossProviderAnalyticsEntities(t *testing.T) {
	snapshots := map[string]core.UsageSnapshot{
		"cursor-ide": {
			ProviderID: "cursor",
			AccountID:  "cursor-ide",
			Timestamp:  time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
			Metrics: map[string]core.Metric{
				"mcp_github_total": {Used: core.Float64Ptr(5)},
			},
			DailySeries: map[string][]core.TimePoint{
				"tokens_client_cli":      {{Date: "2026-04-12", Value: 10}},
				"usage_project_team_one": {{Date: "2026-04-12", Value: 2}},
				"usage_mcp_github":       {{Date: "2026-04-12", Value: 5}},
			},
		},
		"codex-cli": {
			ProviderID: "codex",
			AccountID:  "codex-cli",
			Timestamp:  time.Date(2026, 4, 14, 13, 0, 0, 0, time.UTC),
			Metrics: map[string]core.Metric{
				"mcp_github_total": {Used: core.Float64Ptr(7)},
			},
			DailySeries: map[string][]core.TimePoint{
				"tokens_client_cli":      {{Date: "2026-04-13", Value: 15}},
				"usage_project_team_one": {{Date: "2026-04-13", Value: 3}},
				"usage_mcp_github":       {{Date: "2026-04-13", Value: 7}},
			},
		},
	}

	data := extractCostData(snapshots, "", core.TimeWindow7d)

	if len(data.clients) != 1 {
		t.Fatalf("clients = %d, want 1", len(data.clients))
	}
	if got := data.clients[0].total; got != 25 {
		t.Fatalf("client total = %v, want 25", got)
	}
	if len(data.projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(data.projects))
	}
	if got := data.projects[0].requests; got != 5 {
		t.Fatalf("project requests = %v, want 5", got)
	}
	if len(data.mcpServers) != 1 {
		t.Fatalf("mcp servers = %d, want 1", len(data.mcpServers))
	}
	if got := data.mcpServers[0].calls; got != 12 {
		t.Fatalf("mcp calls = %v, want 12", got)
	}
}

func TestAnalyticsCropSeriesRespectsReferenceTime(t *testing.T) {
	points := []core.TimePoint{
		{Date: "2026-04-07", Value: 4},
		{Date: "2026-04-08", Value: 8},
		{Date: "2026-04-10", Value: 10},
	}

	got := analyticsCropSeries(points, core.TimeWindow3d, time.Date(2026, 4, 10, 15, 0, 0, 0, time.UTC))
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	if got[0].Date != "2026-04-08" || got[1].Date != "2026-04-09" || got[2].Date != "2026-04-10" {
		t.Fatalf("dates = %#v, want 2026-04-08..2026-04-10", got)
	}
	if got[1].Value != 0 {
		t.Fatalf("middle value = %v, want 0 padding", got[1].Value)
	}
}

func TestRenderTimeChartPreservesExplicitWindowSpan(t *testing.T) {
	out := RenderTimeChart(TimeChartSpec{
		Title:             "Window Test",
		Mode:              TimeChartBars,
		Height:            6,
		WindowDays:        5,
		ReferenceTime:     time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC),
		PreserveEmptySpan: true,
		Series: []BrailleSeries{{
			Label: "daily cost",
			Color: colorTeal,
			Points: []core.TimePoint{
				{Date: "2026-04-01", Value: 10},
				{Date: "2026-04-03", Value: 20},
			},
		}},
	}, 64)

	if !strings.Contains(out, "Apr 5") {
		t.Fatalf("expected preserved window end label Apr 5, got:\n%s", out)
	}
}
