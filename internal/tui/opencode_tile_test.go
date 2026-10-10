package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

// opencodeZenTelemetrySnapshot mirrors a real daemon snapshot for an opencode
// account authenticated with only a Zen API key (no console quota), with one
// session of telemetry in the selected window.
func opencodeZenTelemetrySnapshot() core.UsageSnapshot {
	tok := func(v float64) core.Metric { return core.Metric{Used: float64Ptr(v), Unit: "tokens", Window: "1d"} }
	req := func(v float64) core.Metric { return core.Metric{Used: float64Ptr(v), Unit: "requests", Window: "1d"} }
	calls := func(v float64) core.Metric { return core.Metric{Used: float64Ptr(v), Unit: "calls", Window: "1d"} }
	hit, ok := core.CacheHitRatioMetric(163432, 158683, 0, "1d")
	if !ok {
		panic("cache hit ratio undefined")
	}
	return core.UsageSnapshot{
		ProviderID: "opencode",
		AccountID:  "opencode",
		Timestamp:  time.Now(),
		Status:     core.StatusOK,
		Message:    "Auth OK · 82 Zen models",
		Attributes: map[string]string{"auth_scope": "zen", "available_models_count": "82"},
		Metrics: map[string]core.Metric{
			"cache_hit_ratio":                    hit,
			"tool_success_rate":                  {Used: float64Ptr(100), Unit: "%", Window: "1d"},
			"7d_tool_calls":                      calls(16),
			"tool_calls_today":                   calls(16),
			"tool_read":                          calls(9),
			"tool_glob":                          calls(3),
			"tool_bash":                          calls(1),
			"sessions_today":                     {Used: float64Ptr(1), Unit: "sessions", Window: "1d"},
			"messages_today":                     {Used: float64Ptr(11), Unit: "messages", Window: "1d"},
			"window_requests":                    req(11),
			"window_tokens":                      tok(166724),
			"window_cache_read_tokens":           tok(158683),
			"client_opencode_input_tokens":       tok(163432),
			"client_opencode_cached_tokens":      tok(158683),
			"client_opencode_output_tokens":      tok(1504),
			"client_opencode_reasoning_tokens":   tok(1788),
			"client_opencode_total_tokens":       tok(325407),
			"client_opencode_requests":           req(11),
			"client_opencode_sessions":           {Used: float64Ptr(1), Unit: "sessions", Window: "1d"},
			"lang_go":                            req(7),
			"lang_markdown":                      req(1),
			"model_muse_spark_input_tokens":      tok(163432),
			"model_muse_spark_output_tokens":     tok(1504),
			"model_muse_spark_cache_read_tokens": tok(158683),
			"model_muse_spark_requests":          req(11),
		},
	}
}

func TestComputeDisplayInfo_RatioIsNeverPercentUsed(t *testing.T) {
	snap := opencodeZenTelemetrySnapshot()
	di := computeDisplayInfo(snap, dashboardWidget("opencode"), false)
	if strings.Contains(di.summary, "used") || strings.Contains(di.detail, "Cache Hit") {
		t.Fatalf("cache_hit_ratio rendered as quota: summary=%q detail=%q", di.summary, di.detail)
	}
	if di.gaugePercent >= 0 {
		t.Fatalf("gaugePercent = %v, want -1 (no quota)", di.gaugePercent)
	}
	if di.summary != "11 reqs · 166.7k tok" {
		t.Fatalf("summary = %q, want window activity summary", di.summary)
	}
	if di.detail != "16 tool calls · 1 session" {
		t.Fatalf("detail = %q", di.detail)
	}
}

func TestComputeDisplayInfo_QuotaWinsOverRatio(t *testing.T) {
	limit := 100.0
	snap := core.UsageSnapshot{
		ProviderID: "some_provider",
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			// A very high hit ratio sorts first ("a" < "q") and has the lowest
			// remaining percent; it must still be ignored.
			"a_cache_hit_ratio": {Used: float64Ptr(95), Remaining: float64Ptr(5), Limit: &limit, Unit: "%"},
			"quota_requests":    {Used: float64Ptr(20), Remaining: float64Ptr(80), Limit: &limit, Unit: "requests"},
		},
	}
	di := computeDisplayInfo(snap, core.DefaultDashboardWidget(), false)
	if di.summary != "20% used" {
		t.Fatalf("summary = %q, want %q", di.summary, "20% used")
	}
}

func TestRenderTile_OpencodeZenTelemetry(t *testing.T) {
	m := Model{timeWindow: core.TimeWindow1d}
	out := m.renderTile(opencodeZenTelemetrySnapshot(), false, false, 70, 0, 0)
	t.Log("\n" + out)

	for _, want := range []string{
		"11 reqs · 166.7k tok in Today",
		"16 tool calls · 1 session",
		"Clients",
		"Language",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("tile missing %q", want)
		}
	}
	for _, unwanted := range []string{
		"% used",
		"Cache Hit Ratio",
		"No client data",
		"No language data",
		"Client Opencode",
		"7d Tool Calls",
		"Window Requests",
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("tile unexpectedly contains %q", unwanted)
		}
	}
	// The windowed activity is the hero line; it must not be repeated.
	if n := strings.Count(out, "11 reqs"); n != 1 {
		t.Errorf("window activity rendered %d times, want 1", n)
	}
}

func TestRenderTile_OpencodeNoConsoleShowsConnectHint(t *testing.T) {
	snap := opencodeZenTelemetrySnapshot()
	snap.Attributes[core.QuotaConnectHintAttribute] = "Connect OpenCode console for Go quota meters: log into opencode.ai, then Settings → 5 KEYS → opencode → c"
	m := Model{timeWindow: core.TimeWindow1d}
	out := m.renderTile(snap, false, false, 70, 0, 0)
	t.Log("\n" + out)
	for _, want := range []string{"Connect OpenCode console for Go quota meters", "Settings → 5 KEYS → opencode → c", "11 reqs · 166.7k tok in Today"} {
		if !strings.Contains(out, want) {
			t.Errorf("tile missing %q", want)
		}
	}
}

func TestRenderTile_OpencodeGoQuotaGauges(t *testing.T) {
	snap := opencodeZenTelemetrySnapshot()
	now := time.Now()
	snap.Timestamp = now
	pct := func(v float64, window string) core.Metric {
		return core.Metric{Used: float64Ptr(v), Limit: float64Ptr(100), Unit: "percent", Window: window}
	}
	snap.Metrics["rolling_usage"] = pct(12, "rolling-5h")
	snap.Metrics["weekly_usage"] = pct(34, "7d")
	snap.Metrics["monthly_usage_pct"] = pct(49, "month")
	snap.Resets = map[string]time.Time{
		"rolling_usage_reset":     now.Add(2*time.Hour + 10*time.Minute),
		"weekly_usage_reset":      now.Add(3 * 24 * time.Hour),
		"monthly_usage_pct_reset": now.Add(12 * 24 * time.Hour),
	}
	// Even with a stale hint attribute, real gauges win.
	snap.Attributes[core.QuotaConnectHintAttribute] = "Connect OpenCode console for Go quota meters: x"

	m := Model{timeWindow: core.TimeWindow1d}
	out := m.renderTile(snap, false, false, 70, 0, 0)
	t.Log("\n" + out)
	for _, want := range []string{"Usage 5h", "Weekly", "Monthly", "12.0%", "34.0%", "49.0%", "resets"} {
		if !strings.Contains(out, want) {
			t.Errorf("tile missing %q", want)
		}
	}
	for _, unwanted := range []string{"Connect OpenCode console", "Cache Hit"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("tile unexpectedly contains %q", unwanted)
		}
	}
}
