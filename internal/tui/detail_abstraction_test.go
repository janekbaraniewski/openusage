package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func TestClassifyMetric_UsesDetailSectionOrder(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	details := core.DefaultDetailWidget()
	details.Sections = []core.DetailSection{
		{Name: "Usage", Order: 9, Style: core.DetailSectionStyleUsage},
	}

	group, _, order := classifyMetric("rpm", core.Metric{}, widget, details)
	if group != "Usage" {
		t.Fatalf("group = %q, want Usage", group)
	}
	if order != 9 {
		t.Fatalf("order = %d, want 9", order)
	}
}

func TestClassifyMetric_OverrideUsesDetailSectionOrderWhenUnset(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	widget.MetricGroupOverrides["custom_metric"] = core.DashboardMetricGroupOverride{
		Group: "Billing",
		Label: "Custom",
	}

	details := core.DefaultDetailWidget()
	details.Sections = append(details.Sections, core.DetailSection{
		Name:  "Billing",
		Order: 7,
		Style: core.DetailSectionStyleList,
	})

	group, label, order := classifyMetric("custom_metric", core.Metric{}, widget, details)
	if group != "Billing" {
		t.Fatalf("group = %q, want Billing", group)
	}
	if label != "Custom" {
		t.Fatalf("label = %q, want Custom", label)
	}
	if order != 7 {
		t.Fatalf("order = %d, want 7", order)
	}
}

func TestRenderMetricGroup_UnknownSectionFallsBackToList(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	details := core.DefaultDetailWidget()

	used := 3.0
	group := metricGroup{
		title: "Catalog",
		order: 1,
		entries: []metricEntry{
			{
				key:   "models_total",
				label: "Models",
				metric: core.Metric{
					Used: &used,
					Unit: "count",
				},
			},
		},
	}

	var sb strings.Builder
	renderMetricGroup(&sb, group, widget, details, 80, 0.3, 0.1, nil, 0)
	out := sb.String()
	if !strings.Contains(out, "Models") {
		t.Fatalf("output missing metric label: %q", out)
	}
	if !strings.Contains(out, "3 count") {
		t.Fatalf("output missing metric value: %q", out)
	}
}

func TestDetailTabs_IncludesModelsWhenModelUsagePresent(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Metrics:    map[string]core.Metric{"rpm": {Used: core.Float64Ptr(10), Unit: "req"}},
		ModelUsage: []core.ModelUsageRecord{
			{RawModelID: "gpt-4", CostUSD: core.Float64Ptr(5.0)},
		},
	}
	tabs := DetailTabs(snap)
	found := false
	for _, t := range tabs {
		if t == "Models" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Models tab, got tabs: %v", tabs)
	}
}

func TestDetailTabs_IncludesTrendsWhenDailySeriesPresent(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Metrics:    map[string]core.Metric{"rpm": {Used: core.Float64Ptr(10), Unit: "req"}},
		DailySeries: map[string][]core.TimePoint{
			"cost": {
				{Date: "2026-02-20", Value: 5.0},
				{Date: "2026-02-21", Value: 8.0},
				{Date: "2026-02-22", Value: 3.0},
			},
		},
	}
	tabs := DetailTabs(snap)
	found := false
	for _, t := range tabs {
		if t == "Trends" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Trends tab, got tabs: %v", tabs)
	}
}

func TestDetailTabs_NoModelTabWhenEmpty(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Metrics:    map[string]core.Metric{"rpm": {Used: core.Float64Ptr(10), Unit: "req"}},
	}
	tabs := DetailTabs(snap)
	for _, tab := range tabs {
		if tab == "Models" {
			t.Fatal("Models tab should not appear without model data")
		}
		if tab == "Trends" {
			t.Fatal("Trends tab should not appear without daily series")
		}
	}
}

func TestDetailTabs_InfoTabWithAttributes(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Metrics:    map[string]core.Metric{"rpm": {Used: core.Float64Ptr(10), Unit: "req"}},
		Attributes: map[string]string{"plan": "pro"},
	}
	tabs := DetailTabs(snap)
	found := false
	for _, tab := range tabs {
		if tab == "Info" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected Info tab with Attributes, got tabs: %v", tabs)
	}
}

func TestRenderModelsSection_WithModelUsage(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "test",
		Timestamp:  time.Now(),
		ModelUsage: []core.ModelUsageRecord{
			{RawModelID: "claude-opus-4", Canonical: "claude-opus-4", CostUSD: core.Float64Ptr(125.0), InputTokens: core.Float64Ptr(50000), OutputTokens: core.Float64Ptr(30000)},
			{RawModelID: "claude-sonnet-4", Canonical: "claude-sonnet-4", CostUSD: core.Float64Ptr(32.0)},
		},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderModelsSection(&sb, snap, widget, 80)
	out := sb.String()

	if !strings.Contains(out, "claude-opus-4") {
		t.Errorf("expected model name in output, got: %s", out)
	}
	if !strings.Contains(out, "$125") {
		t.Errorf("expected cost in output, got: %s", out)
	}
	// Should contain token breakdown for top model.
	if !strings.Contains(out, "Input") || !strings.Contains(out, "Output") {
		t.Errorf("expected token breakdown in output, got: %s", out)
	}
}

func TestRenderModelsSection_FallbackToMetricCosts(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Metrics: map[string]core.Metric{
			"model_gpt-4_cost": {Used: core.Float64Ptr(50.0), Unit: "USD"},
		},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderModelsSection(&sb, snap, widget, 80)
	out := sb.String()

	if !strings.Contains(out, "gpt-4") {
		t.Errorf("expected model name in fallback output, got: %s", out)
	}
}

func TestRenderTrendsSection_WithCostSeries(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		DailySeries: map[string][]core.TimePoint{
			"cost": {
				{Date: "2026-02-18", Value: 5.0},
				{Date: "2026-02-19", Value: 8.0},
				{Date: "2026-02-20", Value: 12.0},
				{Date: "2026-02-21", Value: 6.0},
				{Date: "2026-02-22", Value: 9.0},
			},
		},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderTrendsSection(&sb, snap, widget, 80)
	out := sb.String()

	// Should contain braille chart characters.
	if len(out) == 0 {
		t.Fatal("expected non-empty trends section output")
	}
}

func TestRenderTrendsSection_EmptyWithInsufficientData(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		DailySeries: map[string][]core.TimePoint{
			"cost": {
				{Date: "2026-02-22", Value: 5.0},
			},
		},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderTrendsSection(&sb, snap, widget, 80)
	out := sb.String()

	if len(out) > 0 {
		t.Errorf("expected empty output for single data point, got: %s", out)
	}
}

func TestRenderInfoSection_SplitsAttributesDiagnosticsRaw(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID:  "test",
		AccountID:   "test",
		Timestamp:   time.Now(),
		Attributes:  map[string]string{"plan": "pro", "email": "test@example.com"},
		Diagnostics: map[string]string{"stale_data": "true"},
		Raw:         map[string]string{"api_version": "v2"},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderInfoSection(&sb, snap, widget, 80)
	out := sb.String()

	if !strings.Contains(out, "Attributes") {
		t.Error("expected Attributes section header")
	}
	if !strings.Contains(out, "Diagnostics") {
		t.Error("expected Diagnostics section header")
	}
	if !strings.Contains(out, "Raw Data") {
		t.Error("expected Raw Data section header")
	}
}

func TestRenderInfoSection_OnlyRaw(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "test",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Raw:        map[string]string{"key": "value"},
	}
	widget := core.DefaultDashboardWidget()
	var sb strings.Builder
	renderInfoSection(&sb, snap, widget, 80)
	out := sb.String()

	if strings.Contains(out, "Attributes") {
		t.Error("should not show Attributes when empty")
	}
	if strings.Contains(out, "Diagnostics") {
		t.Error("should not show Diagnostics when empty")
	}
	if !strings.Contains(out, "Raw Data") {
		t.Error("expected Raw Data section")
	}
}

func TestFilterNonZeroEntries_SuppressesZeros(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	widget.SuppressZeroNonUsageMetrics = true

	zero := float64(0)
	entries := []metricEntry{
		{key: "requests_total", label: "Requests", metric: core.Metric{Used: &zero, Unit: "req"}},
		{key: "cost", label: "Cost", metric: core.Metric{Used: core.Float64Ptr(5.0), Unit: "USD"}},
	}

	result := filterNonZeroEntries(entries, widget)
	if len(result) != 1 {
		t.Fatalf("expected 1 entry after filtering, got %d", len(result))
	}
	if result[0].key != "cost" {
		t.Fatalf("expected cost entry, got %q", result[0].key)
	}
}

func TestFilterNonZeroEntries_KeepsQuotaMetricsWithLimit(t *testing.T) {
	widget := core.DefaultDashboardWidget()
	widget.SuppressZeroNonUsageMetrics = true

	zero := float64(0)
	limit := float64(100)
	entries := []metricEntry{
		{key: "rpm", label: "RPM", metric: core.Metric{Used: &zero, Limit: &limit, Unit: "req"}},
	}

	result := filterNonZeroEntries(entries, widget)
	if len(result) != 1 {
		t.Fatalf("expected quota metric to be kept, got %d entries", len(result))
	}
}

func TestRenderDetailContent_AtVariousWidths(t *testing.T) {
	snap := core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "test",
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used": {Used: core.Float64Ptr(60), Limit: core.Float64Ptr(100), Unit: "%"},
			"spend_limit":       {Used: core.Float64Ptr(45), Limit: core.Float64Ptr(100), Unit: "USD"},
		},
		ModelUsage: []core.ModelUsageRecord{
			{RawModelID: "opus-4", Canonical: "claude-opus-4", CostUSD: core.Float64Ptr(100), InputTokens: core.Float64Ptr(50000), OutputTokens: core.Float64Ptr(25000)},
		},
		DailySeries: map[string][]core.TimePoint{
			"cost": {
				{Date: "2026-02-18", Value: 5},
				{Date: "2026-02-19", Value: 8},
				{Date: "2026-02-20", Value: 12},
			},
		},
		Attributes: map[string]string{"plan": "pro"},
		Raw:        map[string]string{"version": "1.0"},
	}

	widths := []int{40, 60, 80, 120}
	for _, w := range widths {
		// Should not panic at any width.
		out := RenderDetailContent(snap, w, 0.3, 0.1, 0)
		if len(out) == 0 {
			t.Errorf("empty output at width %d", w)
		}
	}
}
