package tui

import (
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers/codex"
)

func TestCodexQuotaDisplay(t *testing.T) {
	now := time.Unix(1789805712, 0).Add(-2 * time.Hour)

	snap := core.NewUsageSnapshot("codex", "test")
	snap.Metrics["cache_hit_ratio"] = core.Metric{Used: core.Float64Ptr(12), Unit: "%"}
	if got := strings.Join(buildCodexQuotaLines(snap, 70, .2, .05, now), "\n"); !strings.Contains(got, "Quotas unavailable") || strings.Contains(got, "12") {
		t.Fatalf("cache must not replace quota: %s", got)
	}
	snap.Metrics["rate_limit_primary"] = core.Metric{Used: core.Float64Ptr(34), Remaining: core.Float64Ptr(66), Limit: core.Float64Ptr(100), Unit: "%", Window: "7d"}
	snap.Resets["rate_limit_primary"] = time.Unix(1789805712, 0)
	snap.Attributes["rate_limit_primary_bucket"] = "codex"
	snap.Metrics["rate_limit_codex_other_primary"] = core.Metric{Used: core.Float64Ptr(0), Remaining: core.Float64Ptr(100), Limit: core.Float64Ptr(100), Unit: "%", Window: "5h"}
	snap.Attributes["rate_limit_codex_other_primary_bucket"] = "Special pool"
	snap.Metrics["rate_limit_codex_weekly_primary"] = core.Metric{Used: core.Float64Ptr(12), Limit: core.Float64Ptr(100), Unit: "%", Window: "7d"}
	snap.Attributes["rate_limit_codex_weekly_primary_bucket"] = "Weekly-only pool"
	for _, width := range []int{32, 72, 120} {
		got := strings.Join(buildCodexQuotaLines(snap, width, .2, .05, now), "\n")
		for _, want := range []string{"Usage 7d", "34.0%", "5h: window unavailable", "Special pool", "Usage 5h", "0.0%", "resets 2h"} {
			if !strings.Contains(got, want) {
				t.Fatalf("width=%d missing %q in %s", width, want, got)
			}
		}
		if strings.Contains(got, "Cache") {
			t.Fatalf("unrelated gauge: %s", got)
		}
		if strings.Count(got, "5h: window unavailable") != 1 || strings.Contains(got, "7d: window unavailable") {
			t.Fatalf("missing-window note belongs only to the main pool: %s", got)
		}
	}
}

func TestCodexDisplayKeepsAuthAndErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		status core.Status
		label  string
	}{
		{core.StatusAuth, "Auth"},
		{core.StatusError, "Error"},
	} {
		snap := core.NewUsageSnapshot("codex", "test")
		snap.Status = tc.status
		snap.Message = "Codex unavailable"
		snap.Metrics["codex_credit_percent_used"] = core.Metric{Used: core.Float64Ptr(57.6), Unit: "%"}
		got := computeDisplayInfo(snap, core.DefaultDashboardWidget(), false)
		if got.tagLabel != tc.label || got.summary == "" {
			t.Fatalf("status=%v display=%+v", tc.status, got)
		}
	}
}

func TestCodexMonthlyCreditFallback(t *testing.T) {
	now := time.Date(2026, 5, 18, 12, 0, 0, 0, time.UTC)
	percent := core.Metric{Used: core.Float64Ptr(6.25), Remaining: core.Float64Ptr(93.75), Limit: core.Float64Ptr(100), Unit: "%", Window: "current-period"}
	credits := core.Metric{Used: core.Float64Ptr(250), Remaining: core.Float64Ptr(3750), Limit: core.Float64Ptr(4000), Unit: "credits", Window: "current-period"}
	quota := core.Metric{Used: core.Float64Ptr(34), Remaining: core.Float64Ptr(66), Limit: core.Float64Ptr(100), Unit: "%", Window: "5h"}
	for _, tc := range []struct {
		name    string
		metrics map[string]core.Metric
		label   string
		pct     float64
		reset   bool
	}{
		{"monthly credits only", map[string]core.Metric{"codex_credit_percent_used": percent, "codex_credit_limit": credits}, "Monthly credits", 6.25, true},
		{"credits and quota windows", map[string]core.Metric{"codex_credit_percent_used": percent, "codex_credit_limit": credits, "rate_limit_primary": quota, "rate_limit_secondary": {Used: core.Float64Ptr(12), Unit: "%", Window: "7d"}}, "Usage 5h", 34, false},
		{"quota windows only", map[string]core.Metric{"rate_limit_primary": quota}, "Usage 5h", 34, false},
		{"quota windows and invalid credits", map[string]core.Metric{"rate_limit_primary": quota, "codex_credit_percent_used": {Used: core.Float64Ptr(math.NaN()), Unit: "%"}}, "Usage 5h", 34, false},
		{"neither", nil, "Quotas unavailable", -1, false},
		{"percentage only", map[string]core.Metric{"codex_credit_percent_used": {Used: core.Float64Ptr(57.6), Unit: "%"}}, "Monthly credits", 57.6, false},
		{"absolute credits only", map[string]core.Metric{"codex_credit_limit": credits}, "Monthly credits", 6.25, true},
		{"invalid percentage with valid absolute credits", map[string]core.Metric{"codex_credit_percent_used": {Used: core.Float64Ptr(math.NaN()), Unit: "%"}, "codex_credit_limit": credits}, "Monthly credits", 6.25, true},
		{"remaining credits only", map[string]core.Metric{"codex_credit_limit": {Remaining: core.Float64Ptr(1696), Limit: core.Float64Ptr(4000), Unit: "credits"}}, "Monthly credits", 57.6, false},
		{"zero usage", map[string]core.Metric{"codex_credit_percent_used": {Used: core.Float64Ptr(0), Unit: "%"}}, "Monthly credits", 0, false},
		{"exhausted credits", map[string]core.Metric{"codex_credit_percent_used": {Used: core.Float64Ptr(100), Unit: "%"}}, "Monthly credits", 100, false},
		{"unusable quota window", map[string]core.Metric{"codex_credit_limit": credits, "rate_limit_primary": {Unit: "%", Window: "5h"}}, "Monthly credits", 6.25, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("codex", "test")
			snap.Status = core.StatusOK
			snap.Diagnostics["rate_limit_warning"] = "live usage payload did not include limit windows"
			for key, metric := range tc.metrics {
				snap.Metrics[key] = metric
			}
			// These metrics must never substitute for a subscription allowance.
			snap.Metrics["cache_hit_ratio"] = core.Metric{Used: core.Float64Ptr(12), Limit: core.Float64Ptr(100), Unit: "%"}
			snap.Metrics["context_window"] = makeUsageMetric(50000, 100000, "session")
			snap.Metrics["today_api_cost"] = core.Metric{Used: core.Float64Ptr(25), Unit: "USD"}
			if tc.reset {
				snap.Resets["codex_credit_limit"] = now.Add(2 * time.Hour)
			}
			widget := codex.New().DashboardWidget()
			m := tileGaugeTestModel(now)
			for _, width := range []int{32, 72, 120} {
				for view, lines := range map[string][]string{
					"dashboard": m.buildTileGaugeLines(snap, widget, width),
					"detail":    buildDetailGaugeLines(snap, widget, width, .3, .15, now),
				} {
					got := strings.Join(lines, "\n")
					if !strings.Contains(got, tc.label) {
						t.Fatalf("%s width=%d missing %q: %s", view, width, tc.label, got)
					}
					if tc.pct >= 0 && !strings.Contains(got, fmt.Sprintf("%.1f%%", tc.pct)) {
						t.Errorf("%s wrong used percentage: %s", view, got)
					}
					if tc.reset != strings.Contains(got, "resets 2h") {
						t.Errorf("%s reset display: %s", view, got)
					}
					if tc.label == "Monthly credits" && (strings.Contains(got, "5h") || strings.Contains(got, "7d") || strings.Contains(got, "Quotas unavailable")) {
						t.Errorf("%s credits mislabeled as rate-limit windows: %s", view, got)
					}
					if tc.label == "Usage 5h" && strings.Contains(got, "Monthly credits") {
						t.Errorf("%s credits replaced quota windows: %s", view, got)
					}
					wantLines := 1
					if tc.reset {
						wantLines++
					}
					if tc.label == "Monthly credits" && (len(lines) != wantLines || strings.Contains(lines[0], "\n")) {
						t.Errorf("%s unexpected gauge lines: %s", view, got)
					}
				}
			}
			for _, hideCosts := range []bool{false, true} {
				info := computeDisplayInfo(snap, widget, hideCosts)
				if tc.label == "Usage 5h" {
					if want := normalizeProviderDisplayInfoType(computeDisplayInfoRaw(snap, widget, hideCosts)); info != want {
						t.Errorf("quota summary changed: got %+v, want %+v", info, want)
					}
					continue
				}
				if math.Abs(info.gaugePercent-tc.pct) > 1e-9 {
					t.Errorf("hideCosts=%v summary gauge=%v, want %v", hideCosts, info.gaugePercent, tc.pct)
				}
				if tc.label == "Monthly credits" && (info.tagLabel != "Credits" || info.summary != fmt.Sprintf("%.1f%% monthly credits used", tc.pct) || strings.Contains(info.detail, "$")) {
					t.Errorf("credit summary: %+v", info)
				}
				if tc.pct < 0 && info.summary != "Quotas unavailable" {
					t.Errorf("missing-data summary: %+v", info)
				}
			}
		})
	}
}

func TestCodexInvalidCreditMetrics(t *testing.T) {
	for _, tc := range []struct {
		name string
		key  string
		met  core.Metric
	}{
		{"empty percentage", "codex_credit_percent_used", core.Metric{Unit: "%"}},
		{"negative percentage", "codex_credit_percent_used", core.Metric{Used: core.Float64Ptr(-1), Unit: "%"}},
		{"percentage over 100", "codex_credit_percent_used", core.Metric{Used: core.Float64Ptr(101), Unit: "%"}},
		{"NaN percentage", "codex_credit_percent_used", core.Metric{Used: core.Float64Ptr(math.NaN()), Unit: "%"}},
		{"infinite percentage", "codex_credit_percent_used", core.Metric{Used: core.Float64Ptr(math.Inf(1)), Unit: "%"}},
		{"wrong percentage unit", "codex_credit_percent_used", core.Metric{Used: core.Float64Ptr(25), Unit: "USD"}},
		{"missing credit limit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(250), Unit: "credits"}},
		{"missing credit usage", "codex_credit_limit", core.Metric{Limit: core.Float64Ptr(4000), Unit: "credits"}},
		{"zero credit limit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(0), Limit: core.Float64Ptr(0), Unit: "credits"}},
		{"negative credit limit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(0), Limit: core.Float64Ptr(-1), Unit: "credits"}},
		{"negative credit usage", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(-1), Limit: core.Float64Ptr(4000), Unit: "credits"}},
		{"NaN credit limit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(250), Limit: core.Float64Ptr(math.NaN()), Unit: "credits"}},
		{"infinite credit limit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(250), Limit: core.Float64Ptr(math.Inf(1)), Unit: "credits"}},
		{"infinite remaining credits", "codex_credit_limit", core.Metric{Remaining: core.Float64Ptr(math.Inf(1)), Limit: core.Float64Ptr(4000), Unit: "credits"}},
		{"overspent credits", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(4001), Limit: core.Float64Ptr(4000), Unit: "credits"}},
		{"wrong credit unit", "codex_credit_limit", core.Metric{Used: core.Float64Ptr(250), Limit: core.Float64Ptr(4000), Unit: "USD"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := core.NewUsageSnapshot("codex", "test")
			snap.Status = core.StatusOK
			snap.Metrics[tc.key] = tc.met
			got := strings.Join(buildCodexQuotaLines(snap, 72, .3, .15, time.Now()), "\n")
			if !strings.Contains(got, "Quotas unavailable") {
				t.Errorf("invalid credit metric became gauge: %s", got)
			}
			if info := computeDisplayInfo(snap, core.DefaultDashboardWidget(), false); info.summary != "Quotas unavailable" || info.gaugePercent != -1 {
				t.Errorf("invalid credit metric became summary: %+v", info)
			}
		})
	}
}

func TestCodexDisplayDoesNotUseCacheAsQuotaSummary(t *testing.T) {
	snap := core.NewUsageSnapshot("codex", "test")
	snap.Status = core.StatusOK
	snap.Metrics["cache_hit_ratio"] = core.Metric{Used: core.Float64Ptr(12), Unit: "%"}
	got := computeDisplayInfo(snap, core.DefaultDashboardWidget(), false)
	if got.summary != "Quotas unavailable" || got.gaugePercent != -1 {
		t.Fatalf("cache metric became quota summary: %+v", got)
	}
}
