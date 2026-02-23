package main

import (
	"context"
	"strings"
	"testing"

	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
)

func TestBuildDemoSnapshots_IncludesAllProviders(t *testing.T) {
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

	for _, provider := range providers.AllProviders() {
		if _, ok := byProvider[provider.ID()]; !ok {
			t.Fatalf("missing demo snapshot for provider %q", provider.ID())
		}
	}
}

func TestBuildDemoSnapshots_WidgetCoverage(t *testing.T) {
	snaps := buildDemoSnapshots()

	type expectation struct {
		hasModelBurnData   bool
		hasClientMixData   bool
		hasPricingShowcase bool
	}

	want := map[string]expectation{
		"claude_code": {hasModelBurnData: true, hasClientMixData: true},
		"codex":       {hasModelBurnData: true, hasClientMixData: true},
		"copilot":     {hasModelBurnData: true, hasClientMixData: true},
		"gemini_cli":  {hasModelBurnData: true, hasClientMixData: true},
		"openrouter":  {hasModelBurnData: true},
		"opencode":    {hasModelBurnData: true, hasPricingShowcase: true},
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
		if exp.hasPricingShowcase {
			if _, ok := snap.Metrics["pricing_input_min_paid_per_1m"]; !ok {
				t.Fatalf("provider %q missing pricing_input_min_paid_per_1m", providerID)
			}
		}
	}
}

func TestBuildDemoAccounts_IncludesAllProviders(t *testing.T) {
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

	for _, provider := range providers.AllProviders() {
		if _, ok := byProvider[provider.ID()]; !ok {
			t.Fatalf("missing account for provider %q", provider.ID())
		}
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
		meta    []string
		resets  []string
		series  []string
	}

	expectations := map[string]providerExpect{
		"openai": {
			metrics: []string{
				"rpm",
				"tpm",
			},
			raw: []string{
				"x-ratelimit-limit-requests",
				"x-ratelimit-limit-tokens",
			},
			resets: []string{
				"rpm",
				"tpm",
			},
		},
		"anthropic": {
			metrics: []string{
				"rpm",
				"tpm",
			},
			raw: []string{
				"anthropic-ratelimit-requests-limit",
				"anthropic-ratelimit-tokens-limit",
			},
			resets: []string{
				"rpm",
				"tpm",
			},
		},
		"alibaba_cloud": {
			metrics: []string{
				"available_balance",
				"tokens_used",
				"model_qwen_max_used",
			},
			meta: []string{
				"billing_cycle_start",
				"billing_cycle_end",
			},
		},
		"groq": {
			metrics: []string{
				"rpm",
				"tpm",
				"rpd",
				"tpd",
			},
			resets: []string{
				"rpm",
				"rpd",
			},
		},
		"mistral": {
			metrics: []string{
				"monthly_budget",
				"monthly_spend",
				"monthly_input_tokens",
			},
			raw: []string{
				"plan",
				"monthly_cost",
			},
		},
		"deepseek": {
			metrics: []string{
				"total_balance",
				"granted_balance",
				"topped_up_balance",
				"rpm",
				"tpm",
			},
			raw: []string{
				"currency",
				"account_available",
			},
		},
		"xai": {
			metrics: []string{
				"credits",
				"rpm",
				"tpm",
			},
			raw: []string{
				"api_key_name",
				"team_id",
			},
		},
		"gemini_api": {
			metrics: []string{
				"available_models",
				"input_token_limit",
				"output_token_limit",
				"rpm",
			},
			raw: []string{
				"models_sample",
				"total_models",
			},
			resets: []string{
				"rpm",
			},
		},
		"gemini_cli": {
			metrics: []string{
				"quota",
				"quota_model_gemini_2_5_pro_requests",
				"tool_calls_success",
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
				"source_ide_requests",
				"composer_accepted_lines",
				"client_ide_sessions",
			},
			raw: []string{
				"billing_cycle_start",
				"billing_cycle_end",
			},
			resets: []string{
				"billing_cycle_end",
			},
			series: []string{
				"usage_source_ide",
				"usage_model_claude-4.6-opus-high-thinking",
			},
		},
		"claude_code": {
			metrics: []string{
				"tool_read_calls",
				"client_demo_alpha_total_tokens",
			},
			raw: []string{
				"block_start",
				"block_end",
			},
			series: []string{
				"tokens_client_demo_alpha",
			},
		},
		"codex": {
			metrics: []string{
				"model_gpt_5_1_codex_max_input_tokens",
				"client_ide_total_tokens",
			},
			series: []string{
				"tokens_client_ide",
			},
		},
		"openrouter": {
			metrics: []string{
				"provider_novita_input_tokens",
				"analytics_7d_tokens",
				"model_novita_moonshotai_kimi-k2_input_tokens",
			},
			series: []string{
				"analytics_tokens",
			},
		},
		"ollama": {
			metrics: []string{
				"usage_five_hour",
				"models_total",
				"requests_today",
				"tool_read_file",
				"model_llama3_1_8b_requests",
			},
			meta: []string{
				"account_email",
				"billing_cycle_start",
				"billing_cycle_end",
				"block_start",
				"block_end",
			},
			resets: []string{
				"usage_five_hour",
				"usage_one_day",
			},
			series: []string{
				"usage_model_llama3_1_8b",
				"usage_source_local",
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
		for _, key := range exp.meta {
			if _, ok := snap.MetaValue(key); !ok {
				t.Fatalf("provider %q missing metadata %q", providerID, key)
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
