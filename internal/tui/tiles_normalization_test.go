package tui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/janekbaraniewski/openusage/internal/core"
)

func float64Ptr(v float64) *float64 {
	return &v
}

func clientByName(clients []clientMixEntry, name string) (clientMixEntry, bool) {
	for _, client := range clients {
		if client.name == name {
			return client, true
		}
	}
	return clientMixEntry{}, false
}

func TestCollectProviderClientMix_NormalizesSourceIntoClient(t *testing.T) {
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"source_composer_requests": {Used: float64Ptr(80), Unit: "requests"},
			"source_cli_requests":      {Used: float64Ptr(20), Unit: "requests"},
			"client_ide_sessions":      {Used: float64Ptr(3), Unit: "sessions"},
		},
		DailySeries: map[string][]core.TimePoint{
			"usage_source_composer": {
				{Date: "2026-02-20", Value: 10},
				{Date: "2026-02-21", Value: 70},
			},
		},
	}

	clients, usedKeys := collectProviderClientMix(snap)

	ide, ok := clientByName(clients, "ide")
	if !ok {
		t.Fatalf("missing normalized ide client from source metrics: %+v", clients)
	}
	if ide.requests != 80 {
		t.Fatalf("ide requests = %.0f, want 80", ide.requests)
	}

	cliAgents, ok := clientByName(clients, "cli_agents")
	if !ok {
		t.Fatalf("missing normalized cli_agents client from source metrics: %+v", clients)
	}
	if cliAgents.requests != 20 {
		t.Fatalf("cli_agents requests = %.0f, want 20", cliAgents.requests)
	}

	if !usedKeys["source_composer_requests"] || !usedKeys["source_cli_requests"] {
		t.Fatalf("expected source metric keys to be consumed, got: %+v", usedKeys)
	}
}

func TestCollectProviderClientMix_PrefersClientSeriesOverSourceSeries(t *testing.T) {
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"client_ide_requests":        {Used: float64Ptr(100), Unit: "requests"},
			"client_cli_agents_requests": {Used: float64Ptr(10), Unit: "requests"},
		},
		DailySeries: map[string][]core.TimePoint{
			"usage_client_ide": {
				{Date: "2026-02-20", Value: 30},
				{Date: "2026-02-21", Value: 70},
			},
			"usage_source_composer": {
				{Date: "2026-02-20", Value: 2},
				{Date: "2026-02-21", Value: 3},
				{Date: "2026-02-22", Value: 4},
			},
			"usage_client_cli_agents": {
				{Date: "2026-02-20", Value: 2},
				{Date: "2026-02-21", Value: 8},
			},
			"usage_source_cli": {
				{Date: "2026-02-20", Value: 1},
				{Date: "2026-02-21", Value: 1},
			},
		},
	}

	clients, _ := collectProviderClientMix(snap)

	ide, ok := clientByName(clients, "ide")
	if !ok {
		t.Fatalf("missing ide client: %+v", clients)
	}
	if len(ide.series) != 2 {
		t.Fatalf("ide series length = %d, want 2", len(ide.series))
	}
	if ide.series[0].Date != "2026-02-20" || ide.series[0].Value != 30 {
		t.Fatalf("unexpected ide day1 point: %+v", ide.series[0])
	}
	if ide.series[1].Date != "2026-02-21" || ide.series[1].Value != 70 {
		t.Fatalf("unexpected ide day2 point: %+v", ide.series[1])
	}

	cli, ok := clientByName(clients, "cli_agents")
	if !ok {
		t.Fatalf("missing cli_agents client: %+v", clients)
	}
	if len(cli.series) != 2 {
		t.Fatalf("cli_agents series length = %d, want 2", len(cli.series))
	}
	if cli.series[0].Date != "2026-02-20" || cli.series[0].Value != 2 {
		t.Fatalf("unexpected cli_agents day1 point: %+v", cli.series[0])
	}
	if cli.series[1].Date != "2026-02-21" || cli.series[1].Value != 8 {
		t.Fatalf("unexpected cli_agents day2 point: %+v", cli.series[1])
	}
}

func TestCollectProviderClientMix_AggregatesSourceSeriesByClient(t *testing.T) {
	snap := core.UsageSnapshot{
		DailySeries: map[string][]core.TimePoint{
			"usage_source_composer": {
				{Date: "2026-02-20", Value: 2},
				{Date: "2026-02-21", Value: 3},
			},
			"usage_source_tab": {
				{Date: "2026-02-20", Value: 1},
				{Date: "2026-02-22", Value: 5},
			},
			"usage_source_cli": {
				{Date: "2026-02-20", Value: 4},
				{Date: "2026-02-21", Value: 1},
			},
			"usage_source_agent": {
				{Date: "2026-02-21", Value: 2},
				{Date: "2026-02-22", Value: 2},
			},
		},
	}

	clients, _ := collectProviderClientMix(snap)

	ide, ok := clientByName(clients, "ide")
	if !ok {
		t.Fatalf("missing ide client: %+v", clients)
	}
	if len(ide.series) != 3 {
		t.Fatalf("ide series length = %d, want 3", len(ide.series))
	}
	if ide.series[0].Date != "2026-02-20" || ide.series[0].Value != 3 {
		t.Fatalf("unexpected ide day1 point: %+v", ide.series[0])
	}
	if ide.series[1].Date != "2026-02-21" || ide.series[1].Value != 3 {
		t.Fatalf("unexpected ide day2 point: %+v", ide.series[1])
	}
	if ide.series[2].Date != "2026-02-22" || ide.series[2].Value != 5 {
		t.Fatalf("unexpected ide day3 point: %+v", ide.series[2])
	}

	cli, ok := clientByName(clients, "cli_agents")
	if !ok {
		t.Fatalf("missing cli_agents client: %+v", clients)
	}
	if len(cli.series) != 3 {
		t.Fatalf("cli_agents series length = %d, want 3", len(cli.series))
	}
	if cli.series[0].Date != "2026-02-20" || cli.series[0].Value != 4 {
		t.Fatalf("unexpected cli_agents day1 point: %+v", cli.series[0])
	}
	if cli.series[1].Date != "2026-02-21" || cli.series[1].Value != 3 {
		t.Fatalf("unexpected cli_agents day2 point: %+v", cli.series[1])
	}
	if cli.series[2].Date != "2026-02-22" || cli.series[2].Value != 2 {
		t.Fatalf("unexpected cli_agents day3 point: %+v", cli.series[2])
	}
}

func TestCollectProviderClientMix_DoesNotDoubleCountRequestsTodayFallback(t *testing.T) {
	snap := core.UsageSnapshot{
		Metrics: map[string]core.Metric{
			"source_cli_requests":       {Used: float64Ptr(367), Unit: "requests"},
			"source_cli_requests_today": {Used: float64Ptr(367), Unit: "requests"},
		},
	}

	clients, _ := collectProviderClientMix(snap)
	cli, ok := clientByName(clients, "cli_agents")
	if !ok {
		t.Fatalf("missing cli_agents client: %+v", clients)
	}
	if cli.requests != 367 {
		t.Fatalf("cli_agents requests = %.0f, want 367", cli.requests)
	}
}

func TestSelectClientMixMode_PrefersTokensThenRequestsThenSessions(t *testing.T) {
	mode, _ := selectClientMixMode([]clientMixEntry{
		{total: 100, requests: 1000, sessions: 10},
	})
	if mode != "tokens" {
		t.Fatalf("mode = %q, want tokens", mode)
	}

	mode, _ = selectClientMixMode([]clientMixEntry{
		{requests: 1000, sessions: 10},
	})
	if mode != "requests" {
		t.Fatalf("mode = %q, want requests", mode)
	}

	mode, _ = selectClientMixMode([]clientMixEntry{
		{sessions: 10},
	})
	if mode != "sessions" {
		t.Fatalf("mode = %q, want sessions", mode)
	}
}

func TestSelectBurnMode_PrefersTokensThenCostThenRequests(t *testing.T) {
	mode, total := selectBurnMode(1200, 4.5, 10)
	if mode != "tokens" || total != 1200 {
		t.Fatalf("mode/total = %q %.1f, want tokens 1200", mode, total)
	}

	mode, total = selectBurnMode(0, 4.5, 10)
	if mode != "cost" || total != 4.5 {
		t.Fatalf("mode/total = %q %.1f, want cost 4.5", mode, total)
	}

	mode, total = selectBurnMode(0, 0, 10)
	if mode != "requests" || total != 10 {
		t.Fatalf("mode/total = %q %.1f, want requests 10", mode, total)
	}
}

func TestCompositionBars_AreStableAcrossCollapsedAndExpanded(t *testing.T) {
	snap := core.UsageSnapshot{
		AccountID: "acct-test",
		Metrics:   make(map[string]core.Metric),
	}

	for i := 1; i <= 6; i++ {
		in := float64(1000 - i*90)
		out := float64(300 - i*20)
		snap.Metrics[fmt.Sprintf("model_m%d_input_tokens", i)] = core.Metric{Used: float64Ptr(in), Unit: "tokens"}
		snap.Metrics[fmt.Sprintf("model_m%d_output_tokens", i)] = core.Metric{Used: float64Ptr(out), Unit: "tokens"}
	}

	for i := 1; i <= 5; i++ {
		in := float64(1800 - i*130)
		out := float64(600 - i*40)
		req := float64(200 - i*12)
		snap.Metrics[fmt.Sprintf("provider_p%d_input_tokens", i)] = core.Metric{Used: float64Ptr(in), Unit: "tokens"}
		snap.Metrics[fmt.Sprintf("provider_p%d_output_tokens", i)] = core.Metric{Used: float64Ptr(out), Unit: "tokens"}
		snap.Metrics[fmt.Sprintf("provider_p%d_requests", i)] = core.Metric{Used: float64Ptr(req), Unit: "requests"}
	}

	for i := 1; i <= 8; i++ {
		req := float64(900 - i*70)
		snap.Metrics[fmt.Sprintf("source_src%d_requests", i)] = core.Metric{Used: float64Ptr(req), Unit: "requests"}
	}

	for i := 1; i <= 6; i++ {
		tok := float64(1500 - i*120)
		req := float64(400 - i*25)
		sess := float64(20 - i)
		snap.Metrics[fmt.Sprintf("client_client%d_total_tokens", i)] = core.Metric{Used: float64Ptr(tok), Unit: "tokens"}
		snap.Metrics[fmt.Sprintf("client_client%d_requests", i)] = core.Metric{Used: float64Ptr(req), Unit: "requests"}
		snap.Metrics[fmt.Sprintf("client_client%d_sessions", i)] = core.Metric{Used: float64Ptr(sess), Unit: "sessions"}
	}

	for i := 1; i <= 7; i++ {
		calls := float64(1200 - i*90)
		snap.Metrics[fmt.Sprintf("tool_tool%d", i)] = core.Metric{Used: float64Ptr(calls), Unit: "calls"}
	}

	type sectionCheck struct {
		name string
		fn   func(core.UsageSnapshot, int, bool) ([]string, map[string]bool)
	}
	checks := []sectionCheck{
		{name: "model", fn: buildProviderModelCompositionLines},
		{name: "provider", fn: buildProviderVendorCompositionLines},
		{name: "source", fn: buildProviderSourceCompositionLines},
		{name: "client", fn: buildProviderClientCompositionLines},
		{name: "tool", fn: buildProviderToolCompositionLines},
	}

	for _, tc := range checks {
		collapsed, _ := tc.fn(snap, 120, false)
		expanded, _ := tc.fn(snap, 120, true)
		if len(collapsed) < 2 || len(expanded) < 2 {
			t.Fatalf("%s section missing expected heading/bar lines; collapsed=%d expanded=%d", tc.name, len(collapsed), len(expanded))
		}
		if collapsed[1] != expanded[1] {
			t.Fatalf("%s bar changed between collapsed and expanded modes", tc.name)
		}
	}
}

func TestBuildModelColorMap_AssignsDistinctColorsForVisibleModels(t *testing.T) {
	models := []modelMixEntry{
		{name: "claude-opus"},
		{name: "claude-sonnet"},
		{name: "gpt-5"},
		{name: "o3"},
		{name: "gemini-2.5"},
	}

	colors := buildModelColorMap(models, "acct-test")
	if len(colors) != len(models) {
		t.Fatalf("color map size = %d, want %d", len(colors), len(models))
	}

	circularDistance := func(a, b, size int) int {
		d := a - b
		if d < 0 {
			d = -d
		}
		if alt := size - d; alt < d {
			d = alt
		}
		return d
	}

	seen := make(map[lipgloss.Color]string)
	limit := len(models)
	if limit > len(modelColorPalette) {
		limit = len(modelColorPalette)
	}
	for i := 0; i < limit; i++ {
		name := models[i].name
		c := colorForModel(colors, name)
		if prev, ok := seen[c]; ok {
			t.Fatalf("duplicate color assigned: %q and %q both got %q", prev, name, c)
		}
		seen[c] = name
	}

	if limit >= 2 {
		base := stablePaletteOffset("model", "acct-test")
		size := len(modelColorPalette)
		for i := 1; i < limit; i++ {
			prevIdx := distributedPaletteIndex(base, i-1, size)
			currIdx := distributedPaletteIndex(base, i, size)
			if d := circularDistance(prevIdx, currIdx, size); d <= 1 {
				t.Fatalf("adjacent palette picks too close: idx[%d]=%d idx[%d]=%d", i-1, prevIdx, i, currIdx)
			}
		}
	}
}

func quotaMetricForTest(usedPercent float64) core.Metric {
	limit := 100.0
	used := usedPercent
	remaining := 100.0 - usedPercent
	return core.Metric{
		Limit:     &limit,
		Used:      &used,
		Remaining: &remaining,
		Unit:      "%",
		Window:    "daily",
	}
}

func TestGeminiPrimaryQuotaMetricKey_UsesHighestModelUsage(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "gemini_cli",
		Metrics: map[string]core.Metric{
			"quota_model_gemini_3_pro_preview_requests":  quotaMetricForTest(98),
			"quota_model_gemini_2_5_flash_requests":      quotaMetricForTest(20),
			"quota_model_gemini_2_5_flash_lite_requests": quotaMetricForTest(75),
			"quota":       quotaMetricForTest(98),
			"quota_flash": quotaMetricForTest(20),
			"quota_pro":   quotaMetricForTest(98),
			"quota_model_gemini_3_pro_preview_vertex_tok":  {Used: float64Ptr(0), Unit: "tokens"},
			"quota_model_gemini_3_pro_preview_vertex_none": {},
		},
	}

	got := geminiPrimaryQuotaMetricKey(snap)
	want := "quota_model_gemini_3_pro_preview_requests"
	if got != want {
		t.Fatalf("geminiPrimaryQuotaMetricKey = %q, want %q", got, want)
	}
}

func TestFilterGeminiPrimaryQuotaReset_OnlyKeepsPrimaryQuota(t *testing.T) {
	now := time.Now()
	entries := []resetEntry{
		{key: "quota_model_gemini_2_5_flash_requests_reset", label: "flash", at: now.Add(2 * time.Hour), dur: 2 * time.Hour},
		{key: "quota_model_gemini_3_pro_preview_requests_reset", label: "pro", at: now.Add(8 * time.Hour), dur: 8 * time.Hour},
		{key: "quota_flash_reset", label: "flash agg", at: now.Add(2 * time.Hour), dur: 2 * time.Hour},
		{key: "usage_seven_day", label: "Usage 7d", at: now.Add(24 * time.Hour), dur: 24 * time.Hour},
	}
	snap := core.UsageSnapshot{
		ProviderID: "gemini_cli",
		Metrics: map[string]core.Metric{
			"quota_model_gemini_3_pro_preview_requests": quotaMetricForTest(98),
			"quota_model_gemini_2_5_flash_requests":     quotaMetricForTest(20),
		},
	}

	filtered := filterGeminiPrimaryQuotaReset(entries, snap)
	if len(filtered) != 2 {
		t.Fatalf("filtered len = %d, want 2", len(filtered))
	}

	hasPrimary := false
	hasNonQuota := false
	for _, entry := range filtered {
		if entry.key == "quota_model_gemini_3_pro_preview_requests_reset" {
			hasPrimary = true
		}
		if entry.key == "usage_seven_day" {
			hasNonQuota = true
		}
		if isGeminiQuotaResetKey(entry.key) && entry.key != "quota_model_gemini_3_pro_preview_requests_reset" {
			t.Fatalf("unexpected secondary quota reset kept: %q", entry.key)
		}
	}
	if !hasPrimary {
		t.Fatal("expected primary quota reset to be kept")
	}
	if !hasNonQuota {
		t.Fatal("expected non-quota reset to be preserved")
	}
}

func TestBuildGeminiOtherQuotaLines_ExcludesPrimaryAndUsesRemaining(t *testing.T) {
	now := time.Now()
	snap := core.UsageSnapshot{
		ProviderID: "gemini_cli",
		Metrics: map[string]core.Metric{
			"quota_model_gemini_3_pro_preview_requests":  quotaMetricForTest(98),
			"quota_model_gemini_2_5_flash_lite_requests": quotaMetricForTest(75),
			"quota_model_gemini_2_0_flash_requests":      quotaMetricForTest(40),
		},
		Resets: map[string]time.Time{
			"quota_model_gemini_3_pro_preview_requests_reset":  now.Add(8 * time.Hour),
			"quota_model_gemini_2_5_flash_lite_requests_reset": now.Add(2 * time.Hour),
			"quota_model_gemini_2_0_flash_requests_reset":      now.Add(6 * time.Hour),
		},
	}

	lines, usedKeys := buildGeminiOtherQuotaLines(snap, 120)
	if len(lines) != 3 {
		t.Fatalf("lines len = %d, want 3 (heading + 2 rows)", len(lines))
	}
	if !strings.Contains(lines[0], "Other Quotas") {
		t.Fatalf("heading line missing 'Other Quotas': %q", lines[0])
	}

	if usedKeys["quota_model_gemini_3_pro_preview_requests"] {
		t.Fatal("primary quota metric should not be included in other quotas")
	}
	if !usedKeys["quota_model_gemini_2_5_flash_lite_requests"] {
		t.Fatal("expected flash lite quota metric in other quotas")
	}
	if !usedKeys["quota_model_gemini_2_0_flash_requests"] {
		t.Fatal("expected flash quota metric in other quotas")
	}
}
