package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

func TestBuildDemoSnapshots_IncludesAllDemoProviders(t *testing.T) {
	snaps := buildDemoSnapshots()
	if len(snaps) == 0 {
		t.Fatal("buildDemoSnapshots returned no snapshots")
	}

	byProvider := make(map[string]string)
	for accountID, snap := range snaps {
		if snap.AccountID == "" {
			t.Fatalf("snapshot for key %q has empty account id", accountID)
		}
		if accountID != snap.AccountID {
			t.Fatalf("snapshot key/account mismatch: key=%q account=%q", accountID, snap.AccountID)
		}
		if snap.ProviderID == "" {
			t.Fatalf("snapshot %q has empty provider id", accountID)
		}
		if snap.Status == "" {
			t.Fatalf("snapshot %q has empty status", accountID)
		}
		if snap.Metrics == nil {
			t.Fatalf("snapshot %q has nil metrics map", accountID)
		}
		if existing, ok := byProvider[snap.ProviderID]; ok {
			t.Fatalf("provider %q appears multiple times (%q, %q)", snap.ProviderID, existing, accountID)
		}
		byProvider[snap.ProviderID] = accountID
	}

	for providerID := range demoProviderIDs {
		if _, ok := byProvider[providerID]; !ok {
			t.Fatalf("missing demo snapshot for provider %q", providerID)
		}
	}

	if len(snaps) != len(demoProviderIDs) {
		t.Fatalf("expected %d snapshots, got %d", len(demoProviderIDs), len(snaps))
	}
}

func TestBuildDemoSnapshots_WidgetCoverage(t *testing.T) {
	snaps := buildDemoSnapshots()

	type expectation struct {
		hasModelBurnData bool
		hasClientMixData bool
	}

	want := map[string]expectation{
		"claude_code": {hasModelBurnData: true, hasClientMixData: true},
		"codex":       {hasModelBurnData: true, hasClientMixData: true},
		"copilot":     {hasModelBurnData: true, hasClientMixData: true},
		"gemini_cli":  {hasModelBurnData: true, hasClientMixData: true},
		"openrouter":  {hasModelBurnData: true, hasClientMixData: true},
	}

	for providerID, exp := range want {
		snap, ok := snapshotByProvider(snaps, providerID)
		if !ok {
			t.Fatalf("missing snapshot for provider %q", providerID)
		}
		if exp.hasModelBurnData && !hasModelBurnMetrics(snap) {
			t.Fatalf("provider %q missing model burn metrics", providerID)
		}
		if exp.hasClientMixData && !hasClientMixMetrics(snap) {
			t.Fatalf("provider %q missing client mix metrics", providerID)
		}
	}
}

func TestBuildDemoAccounts_IncludesAllDemoProviders(t *testing.T) {
	accounts := buildDemoAccounts()
	if len(accounts) == 0 {
		t.Fatal("buildDemoAccounts returned no accounts")
	}

	byProvider := make(map[string]core.AccountConfig, len(accounts))
	for _, account := range accounts {
		if account.ID == "" {
			t.Fatalf("account for provider %q has empty ID", account.Provider)
		}
		if account.Provider == "" {
			t.Fatalf("account %q has empty provider ID", account.ID)
		}
		if _, ok := byProvider[account.Provider]; ok {
			t.Fatalf("duplicate account for provider %q", account.Provider)
		}
		byProvider[account.Provider] = account
	}

	for providerID := range demoProviderIDs {
		if _, ok := byProvider[providerID]; !ok {
			t.Fatalf("missing account for provider %q", providerID)
		}
	}

	if len(accounts) != len(demoProviderIDs) {
		t.Fatalf("expected %d accounts, got %d", len(demoProviderIDs), len(accounts))
	}
}

func TestBuildDemoProviders_FetchesMockedSnapshots(t *testing.T) {
	wrapped := buildDemoProviders(providers.AllProviders())
	if len(wrapped) == 0 {
		t.Fatal("buildDemoProviders returned no providers")
	}

	byProvider := make(map[string]core.UsageProvider, len(wrapped))
	for _, provider := range wrapped {
		byProvider[provider.ID()] = provider
	}

	for _, account := range buildDemoAccounts() {
		provider, ok := byProvider[account.Provider]
		if !ok {
			t.Fatalf("missing wrapped provider %q", account.Provider)
		}

		snap, err := provider.Fetch(context.Background(), account)
		if err != nil {
			t.Fatalf("fetch for provider %q failed: %v", account.Provider, err)
		}
		if snap.ProviderID != account.Provider {
			t.Fatalf("provider mismatch for account %q: got %q want %q", account.ID, snap.ProviderID, account.Provider)
		}
		if snap.AccountID != account.ID {
			t.Fatalf("account mismatch for provider %q: got %q want %q", account.Provider, snap.AccountID, account.ID)
		}
		if snap.Status == "" {
			t.Fatalf("empty status for provider %q", account.Provider)
		}
		if snap.Metrics == nil {
			t.Fatalf("nil metrics for provider %q", account.Provider)
		}
	}
}

func TestBuildDemoSnapshots_RichProviderDetails(t *testing.T) {
	snaps := buildDemoSnapshots()

	type providerExpect struct {
		metrics []string
		raw     []string
		resets  []string
		series  []string
	}

	expectations := map[string]providerExpect{
		"gemini_cli": {
			metrics: []string{
				"quota",
				"quota_model_gemini_2_5_pro_requests",
				"tool_calls_success",
				"tool_calls_total",
				"tool_success_rate",
				"composer_lines_added",
				"composer_files_changed",
				"lang_go",
			},
			raw: []string{
				"language_usage",
			},
			resets: []string{
				"quota_model_gemini_2_5_pro_requests_reset",
			},
			series: []string{
				"analytics_tokens",
			},
		},
		"cursor": {
			metrics: []string{
				"interface_composer",
				"composer_accepted_lines",
				"tool_calls_total",
			},
			raw: []string{
				"billing_cycle_start",
				"billing_cycle_end",
			},
			resets: []string{
				"billing_cycle_end",
			},
			series: []string{
				"usage_model_claude-4.6-opus-high-thinking",
			},
		},
		"claude_code": {
			metrics: []string{
				"tool_bash",
				"client_api_server_total_tokens",
				"project_platform_core_requests",
				"lang_go",
				"composer_lines_added",
				"total_prompts",
			},
			raw: []string{
				"block_start",
				"block_end",
				"language_usage",
				"project_usage",
			},
			series: []string{
				"analytics_tokens",
				"tokens_client_api_server",
				"usage_model_synthetic",
				"usage_project_platform_core",
			},
		},
		"codex": {
			metrics: []string{
				"model_gpt_5_4_input_tokens",
				"client_cli_total_tokens",
				"project_dashboard_shell_requests",
			},
			raw: []string{
				"project_usage",
			},
			series: []string{
				"analytics_tokens",
				"tokens_client_cli",
				"usage_project_dashboard_shell",
			},
		},
		"openrouter": {
			metrics: []string{
				"analytics_7d_tokens",
				"model_qwen_qwen3-coder-flash_cost_usd",
				"client_recipe_blog_total_tokens",
				"lang_code",
				"tool_calls_total",
			},
			raw: []string{
				"client_usage",
				"tool_usage",
				"language_usage",
			},
			series: []string{
				"analytics_tokens",
				"tokens_client_recipe_blog",
			},
		},
		"copilot": {
			metrics: []string{
				"gh_core_rpm",
				"gh_graphql_rpm",
				"model_claude_haiku_4_5_input_tokens",
				"client_vscode_total_tokens",
				"tool_calls_total",
				"tool_success_rate",
				"composer_lines_added",
				"composer_files_changed",
				"lang_go",
			},
			raw: []string{
				"language_usage",
			},
			resets: []string{
				"gh_core_rpm_reset",
			},
			series: []string{
				"tokens_client_vscode",
			},
		},
	}

	for providerID, exp := range expectations {
		snap, ok := snapshotByProvider(snaps, providerID)
		if !ok {
			t.Fatalf("missing snapshot for provider %q", providerID)
		}

		for _, key := range exp.metrics {
			if _, ok := snap.Metrics[key]; !ok {
				t.Fatalf("provider %q missing metric %q", providerID, key)
			}
		}
		for _, key := range exp.raw {
			if _, ok := snap.Raw[key]; !ok {
				t.Fatalf("provider %q missing raw %q", providerID, key)
			}
		}
		for _, key := range exp.resets {
			if _, ok := snap.Resets[key]; !ok {
				t.Fatalf("provider %q missing reset %q", providerID, key)
			}
		}
		for _, key := range exp.series {
			if _, ok := snap.DailySeries[key]; !ok {
				t.Fatalf("provider %q missing daily series %q", providerID, key)
			}
		}
	}
}

func TestBuildDemoSnapshots_UsesNonLinearDailyPatterns(t *testing.T) {
	snaps := buildDemoSnapshots()

	cases := []struct {
		providerID  string
		key         string
		minPoints   int
		minSpanDays int
	}{
		{providerID: "claude_code", key: "analytics_requests", minPoints: 10, minSpanDays: 14},
		{providerID: "codex", key: "analytics_requests", minPoints: 3, minSpanDays: 6},
		{providerID: "cursor", key: "analytics_tokens", minPoints: 5, minSpanDays: 7},
		{providerID: "openrouter", key: "analytics_tokens", minPoints: 8, minSpanDays: 16},
	}

	for _, tc := range cases {
		snap, ok := snapshotByProvider(snaps, tc.providerID)
		if !ok {
			t.Fatalf("missing snapshot for provider %q", tc.providerID)
		}
		pts := snap.DailySeries[tc.key]
		if len(pts) < tc.minPoints {
			t.Fatalf("provider %q series %q too short: got %d want >= %d", tc.providerID, tc.key, len(pts), tc.minPoints)
		}
		if span := seriesSpanDays(t, pts); span < tc.minSpanDays {
			t.Fatalf("provider %q series %q spans only %d days; want >= %d", tc.providerID, tc.key, span, tc.minSpanDays)
		}
		if isStrictlyIncreasing(pts) {
			t.Fatalf("provider %q series %q is still a straight ramp", tc.providerID, tc.key)
		}
	}
}

func snapshotByProvider(snaps map[string]core.UsageSnapshot, providerID string) (core.UsageSnapshot, bool) {
	for _, snap := range snaps {
		if snap.ProviderID == providerID {
			return snap, true
		}
	}
	return core.UsageSnapshot{}, false
}

func hasModelBurnMetrics(snap core.UsageSnapshot) bool {
	for key, m := range snap.Metrics {
		if m.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_cost_usd") || strings.HasSuffix(key, "_cost")) {
			return true
		}
		if strings.HasPrefix(key, "model_") && (strings.HasSuffix(key, "_input_tokens") || strings.HasSuffix(key, "_output_tokens")) {
			return true
		}
	}
	return false
}

func hasClientMixMetrics(snap core.UsageSnapshot) bool {
	for key, m := range snap.Metrics {
		if m.Used == nil {
			continue
		}
		if strings.HasPrefix(key, "client_") && strings.HasSuffix(key, "_total_tokens") {
			return true
		}
	}
	return false
}

func seriesSpanDays(t *testing.T, pts []core.TimePoint) int {
	t.Helper()
	if len(pts) < 2 {
		return 0
	}
	first, err := time.Parse("2006-01-02", pts[0].Date)
	if err != nil {
		t.Fatalf("parse first date %q: %v", pts[0].Date, err)
	}
	last, err := time.Parse("2006-01-02", pts[len(pts)-1].Date)
	if err != nil {
		t.Fatalf("parse last date %q: %v", pts[len(pts)-1].Date, err)
	}
	return int(last.Sub(first).Hours() / 24)
}

func isStrictlyIncreasing(pts []core.TimePoint) bool {
	if len(pts) < 2 {
		return false
	}
	for i := 1; i < len(pts); i++ {
		if pts[i].Value <= pts[i-1].Value {
			return false
		}
	}
	return true
}
