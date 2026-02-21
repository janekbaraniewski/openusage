package tui

import (
	"strings"
	"testing"

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
	renderMetricGroup(&sb, group, widget, details, 80, 0.3, 0.1, nil)
	out := sb.String()
	if !strings.Contains(out, "Models") {
		t.Fatalf("output missing metric label: %q", out)
	}
	if !strings.Contains(out, "3 count") {
		t.Fatalf("output missing metric value: %q", out)
	}
}
