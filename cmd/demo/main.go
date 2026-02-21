package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func ptr(v float64) *float64 { return &v }

func main() {
	log.SetOutput(io.Discard)

	interval := 5 * time.Second
	accounts := buildDemoAccounts()
	engine := core.NewEngine(interval)
	engine.SetAccounts(accounts)
	for _, provider := range buildDemoProviders(providers.AllProviders()) {
		engine.RegisterProvider(provider)
	}

	model := tui.NewModel(
		0.20,
		0.05,
		false,
		config.DashboardConfig{},
		accounts,
	)
	model.SetOnAddAccount(engine.AddAccount)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	engine.OnUpdate(func(snaps map[string]core.UsageSnapshot) {
		p.Send(tui.SnapshotsMsg(snaps))
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go engine.Run(ctx)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

type demoProvider struct {
	base core.UsageProvider
}

func buildDemoProviders(realProviders []core.UsageProvider) []core.UsageProvider {
	out := make([]core.UsageProvider, 0, len(realProviders))
	for _, provider := range realProviders {
		out = append(out, &demoProvider{base: provider})
	}
	return out
}

func buildDemoAccounts() []core.AccountConfig {
	providerList := providers.AllProviders()
	accounts := make([]core.AccountConfig, 0, len(providerList))
	seenAccountIDs := make(map[string]bool, len(providerList))
	for _, provider := range providerList {
		spec := provider.Spec()
		accountID := demoAccountID(provider.ID())
		if accountID == "" {
			accountID = spec.Auth.DefaultAccountID
		}
		if accountID == "" {
			accountID = provider.ID()
		}
		if seenAccountIDs[accountID] {
			accountID = provider.ID()
		}

		accounts = append(accounts, core.AccountConfig{
			ID:        accountID,
			Provider:  provider.ID(),
			Auth:      string(spec.Auth.Type),
			APIKeyEnv: spec.Auth.APIKeyEnv,
		})
		seenAccountIDs[accountID] = true
	}
	return accounts
}

func (p *demoProvider) ID() string {
	return p.base.ID()
}

func (p *demoProvider) Describe() core.ProviderInfo {
	return p.base.Describe()
}

func (p *demoProvider) Spec() core.ProviderSpec {
	return p.base.Spec()
}

func (p *demoProvider) DashboardWidget() core.DashboardWidget {
	return p.base.DashboardWidget()
}

func (p *demoProvider) DetailWidget() core.DetailWidget {
	return p.base.DetailWidget()
}

func (p *demoProvider) Fetch(_ context.Context, acct core.AccountConfig) (core.UsageSnapshot, error) {
	snaps := buildDemoSnapshots()
	if snap, ok := snaps[acct.ID]; ok && snap.ProviderID == p.base.ID() {
		return forceAccountAndProvider(snap, acct.ID, p.base.ID()), nil
	}

	for _, snap := range snaps {
		if snap.ProviderID == p.base.ID() {
			return forceAccountAndProvider(snap, acct.ID, p.base.ID()), nil
		}
	}

	now := time.Now()
	snap := demoDefaultSnapshot(p.base.ID(), acct.ID, now)
	return forceAccountAndProvider(snap, acct.ID, p.base.ID()), nil
}

func forceAccountAndProvider(snap core.UsageSnapshot, accountID, providerID string) core.UsageSnapshot {
	snap.AccountID = accountID
	snap.ProviderID = providerID
	return snap
}

func buildDemoSnapshots() map[string]core.UsageSnapshot {
	now := time.Now()
	rng := rand.New(rand.NewSource(now.UnixNano()))
	snaps := make(map[string]core.UsageSnapshot)

	// openai
	snaps["openai"] = core.UsageSnapshot{
		ProviderID: "openai",
		AccountID:  "openai",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rpm": {
				Limit: ptr(10000), Remaining: ptr(7820), Used: ptr(2180),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(2000000), Remaining: ptr(1664000), Used: ptr(336000),
				Unit: "tokens", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(34 * time.Second),
			"tpm": now.Add(34 * time.Second),
		},
		Raw: map[string]string{
			"x-ratelimit-limit-requests":     "10000",
			"x-ratelimit-remaining-requests": "7820",
			"x-ratelimit-reset-requests":     "34s",
			"x-ratelimit-limit-tokens":       "2000000",
			"x-ratelimit-remaining-tokens":   "1664000",
			"x-ratelimit-reset-tokens":       "34s",
		},
		Message: "OpenAI rate limits healthy",
	}

	// anthropic
	snaps["anthropic"] = core.UsageSnapshot{
		ProviderID: "anthropic",
		AccountID:  "anthropic",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rpm": {
				Limit: ptr(4000), Remaining: ptr(3510), Used: ptr(490),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(400000), Remaining: ptr(308000), Used: ptr(92000),
				Unit: "tokens", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(21 * time.Second),
			"tpm": now.Add(21 * time.Second),
		},
		Raw: map[string]string{
			"anthropic-ratelimit-requests-limit":     "4000",
			"anthropic-ratelimit-requests-remaining": "3510",
			"anthropic-ratelimit-requests-reset":     "21s",
			"anthropic-ratelimit-tokens-limit":       "400000",
			"anthropic-ratelimit-tokens-remaining":   "308000",
			"anthropic-ratelimit-tokens-reset":       "21s",
		},
		Message: "Anthropic limits and token budget available",
	}

	// alibaba-cloud
	snaps["alibaba_cloud"] = core.UsageSnapshot{
		ProviderID: "alibaba_cloud",
		AccountID:  "alibaba_cloud",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"available_balance": {Limit: ptr(1200), Remaining: ptr(780), Used: ptr(420), Unit: "USD", Window: "current"},
			"credit_balance":    {Limit: ptr(1500), Remaining: ptr(1090), Used: ptr(410), Unit: "USD", Window: "current"},
			"spend_limit":       {Limit: ptr(2000), Remaining: ptr(1258), Used: ptr(742), Unit: "USD", Window: "current"},
			"daily_spend":       {Used: ptr(19.8), Unit: "USD", Window: "1d"},
			"monthly_spend":     {Used: ptr(742), Unit: "USD", Window: "30d"},
			"rpm":               {Limit: ptr(3000), Remaining: ptr(2430), Used: ptr(570), Unit: "requests", Window: "1m"},
			"tpm":               {Limit: ptr(900000), Remaining: ptr(631000), Used: ptr(269000), Unit: "tokens", Window: "1m"},
			"tokens_used":       {Used: ptr(1.62e7), Unit: "tokens", Window: "current"},
			"requests_used":     {Used: ptr(55200), Unit: "requests", Window: "current"},
			"model_qwen_max_usage_pct": {
				Used: ptr(67), Unit: "%", Window: "current",
			},
			"model_qwen_max_used": {
				Used: ptr(1340), Limit: ptr(2000), Unit: "units", Window: "current",
			},
			"model_deepseek_r1_usage_pct": {
				Used: ptr(38), Unit: "%", Window: "current",
			},
			"model_deepseek_r1_used": {
				Used: ptr(760), Limit: ptr(2000), Unit: "units", Window: "current",
			},
		},
		Attributes: map[string]string{
			"billing_cycle_start": now.Add(-10 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"billing_cycle_end":   now.Add(20 * 24 * time.Hour).UTC().Format(time.RFC3339),
		},
		Raw: map[string]string{
			"request_id": "demo-ali-usage-4289",
		},
		Message: "Alibaba quotas and billing data available",
	}

	// groq
	snaps["groq"] = core.UsageSnapshot{
		ProviderID: "groq",
		AccountID:  "groq",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"rpm": {
				Limit: ptr(30000), Remaining: ptr(28760), Used: ptr(1240),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(900000), Remaining: ptr(794000), Used: ptr(106000),
				Unit: "tokens", Window: "1m",
			},
			"rpd": {
				Limit: ptr(500000), Remaining: ptr(482600), Used: ptr(17400),
				Unit: "requests", Window: "1d",
			},
			"tpd": {
				Limit: ptr(9000000), Remaining: ptr(8220000), Used: ptr(780000),
				Unit: "tokens", Window: "1d",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(41 * time.Second),
			"tpm": now.Add(41 * time.Second),
			"rpd": now.Add(13*time.Hour + 18*time.Minute),
			"tpd": now.Add(13*time.Hour + 18*time.Minute),
		},
		Raw: map[string]string{
			"x-ratelimit-limit-requests-day":     "500000",
			"x-ratelimit-remaining-requests-day": "482600",
			"x-ratelimit-limit-tokens-day":       "9000000",
			"x-ratelimit-remaining-tokens-day":   "8220000",
		},
		Message: "Remaining: 28760/30000 RPM, 482600/500000 RPD",
	}

	// mistral
	snaps["mistral"] = core.UsageSnapshot{
		ProviderID: "mistral",
		AccountID:  "mistral",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"monthly_budget":       {Limit: ptr(300.0), Unit: "EUR", Window: "1mo"},
			"monthly_spend":        {Limit: ptr(300.0), Remaining: ptr(188.4), Used: ptr(111.6), Unit: "EUR", Window: "1mo"},
			"credit_balance":       {Remaining: ptr(188.4), Unit: "EUR", Window: "current"},
			"monthly_input_tokens": {Used: ptr(5920000), Unit: "tokens", Window: "1mo"},
			"monthly_output_tokens": {
				Used: ptr(831000), Unit: "tokens", Window: "1mo",
			},
			"rpm": {
				Limit: ptr(8000), Remaining: ptr(7340), Used: ptr(660),
				Unit: "requests", Window: "1m",
			},
			"rpm_alt": {
				Limit: ptr(8000), Remaining: ptr(7340), Used: ptr(660),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(1000000), Remaining: ptr(888000), Used: ptr(112000),
				Unit: "tokens", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm":     now.Add(39 * time.Second),
			"rpm_alt": now.Add(39 * time.Second),
			"tpm":     now.Add(39 * time.Second),
		},
		Raw: map[string]string{
			"plan":         "La Plateforme Team",
			"monthly_cost": "111.6000 EUR",
		},
		Message: "Mistral monthly spend: 111.60 EUR",
	}

	// deepseek
	snaps["deepseek"] = core.UsageSnapshot{
		ProviderID: "deepseek",
		AccountID:  "deepseek",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"total_balance":     {Remaining: ptr(428.90), Unit: "CNY", Window: "current"},
			"granted_balance":   {Remaining: ptr(100.00), Unit: "CNY", Window: "current"},
			"topped_up_balance": {Remaining: ptr(328.90), Unit: "CNY", Window: "current"},
			"rpm": {
				Limit: ptr(6000), Remaining: ptr(5820), Used: ptr(180),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(600000), Remaining: ptr(546000), Used: ptr(54000),
				Unit: "tokens", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(28 * time.Second),
			"tpm": now.Add(28 * time.Second),
		},
		Raw: map[string]string{
			"currency":          "CNY",
			"account_available": "true",
		},
		Message: "Balance: 428.90 CNY",
	}

	// xai
	snaps["xai"] = core.UsageSnapshot{
		ProviderID: "xai",
		AccountID:  "xai",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credits": {
				Limit: ptr(500.00), Remaining: ptr(372.19), Used: ptr(127.81), Unit: "USD", Window: "current",
			},
			"rpm": {
				Limit: ptr(6000), Remaining: ptr(5570), Used: ptr(430),
				Unit: "requests", Window: "1m",
			},
			"tpm": {
				Limit: ptr(900000), Remaining: ptr(756000), Used: ptr(144000),
				Unit: "tokens", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(36 * time.Second),
			"tpm": now.Add(36 * time.Second),
		},
		Raw: map[string]string{
			"api_key_name": "prod-key",
			"team_id":      "team_7f4a2",
		},
		Message: "$372.19 remaining",
	}

	// gemini-api
	snaps["gemini-api"] = core.UsageSnapshot{
		ProviderID: "gemini_api",
		AccountID:  "gemini-api",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"available_models":   {Used: ptr(37), Unit: "models", Window: "current"},
			"input_token_limit":  {Limit: ptr(1048576), Unit: "tokens", Window: "per-request"},
			"output_token_limit": {Limit: ptr(8192), Unit: "tokens", Window: "per-request"},
			"rpm": {
				Limit: ptr(60), Remaining: ptr(44), Used: ptr(16),
				Unit: "requests", Window: "1m",
			},
		},
		Resets: map[string]time.Time{
			"rpm": now.Add(43 * time.Second),
		},
		Raw: map[string]string{
			"models_sample": "gemini-2.5-flash, gemini-2.5-pro, gemini-2.0-flash",
			"total_models":  "37",
			"model_name":    "Gemini 2.5 Flash",
		},
		Message: "auth OK; 37 models available",
	}

	// ollama
	snaps["ollama"] = core.UsageSnapshot{
		ProviderID: "ollama",
		AccountID:  "ollama",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"usage_five_hour":                {Used: ptr(34), Unit: "%", Window: "5h"},
			"usage_one_day":                  {Used: ptr(62), Unit: "%", Window: "1d"},
			"models_total":                   {Used: ptr(24), Remaining: ptr(24), Unit: "models", Window: "current"},
			"models_local":                   {Used: ptr(18), Remaining: ptr(18), Unit: "models", Window: "current"},
			"models_cloud":                   {Used: ptr(6), Remaining: ptr(6), Unit: "models", Window: "current"},
			"loaded_models":                  {Used: ptr(3), Remaining: ptr(3), Unit: "models", Window: "current"},
			"loaded_vram_bytes":              {Used: ptr(17.2e9), Remaining: ptr(17.2e9), Unit: "bytes", Window: "current"},
			"model_storage_bytes":            {Used: ptr(83.4e9), Remaining: ptr(83.4e9), Unit: "bytes", Window: "current"},
			"context_window":                 {Used: ptr(131072), Remaining: ptr(131072), Unit: "tokens", Window: "current"},
			"configured_context_length":      {Used: ptr(65536), Remaining: ptr(65536), Unit: "tokens", Window: "current"},
			"requests_today":                 {Used: ptr(412), Remaining: ptr(412), Unit: "requests", Window: "today"},
			"messages_today":                 {Used: ptr(388), Remaining: ptr(388), Unit: "messages", Window: "today"},
			"sessions_today":                 {Used: ptr(63), Remaining: ptr(63), Unit: "sessions", Window: "today"},
			"tool_calls_today":               {Used: ptr(157), Remaining: ptr(157), Unit: "calls", Window: "today"},
			"requests_5h":                    {Used: ptr(138), Remaining: ptr(138), Unit: "requests", Window: "5h"},
			"messages_5h":                    {Used: ptr(129), Remaining: ptr(129), Unit: "messages", Window: "5h"},
			"sessions_5h":                    {Used: ptr(21), Remaining: ptr(21), Unit: "sessions", Window: "5h"},
			"tool_calls_5h":                  {Used: ptr(48), Remaining: ptr(48), Unit: "calls", Window: "5h"},
			"requests_1d":                    {Used: ptr(412), Remaining: ptr(412), Unit: "requests", Window: "1d"},
			"messages_1d":                    {Used: ptr(388), Remaining: ptr(388), Unit: "messages", Window: "1d"},
			"sessions_1d":                    {Used: ptr(63), Remaining: ptr(63), Unit: "sessions", Window: "1d"},
			"tool_calls_1d":                  {Used: ptr(157), Remaining: ptr(157), Unit: "calls", Window: "1d"},
			"recent_requests":                {Used: ptr(512), Remaining: ptr(512), Unit: "requests", Window: "24h"},
			"requests_7d":                    {Used: ptr(2310), Remaining: ptr(2310), Unit: "requests", Window: "7d"},
			"chat_requests_today":            {Used: ptr(271), Remaining: ptr(271), Unit: "requests", Window: "today"},
			"generate_requests_today":        {Used: ptr(141), Remaining: ptr(141), Unit: "requests", Window: "today"},
			"chat_requests_5h":               {Used: ptr(90), Remaining: ptr(90), Unit: "requests", Window: "5h"},
			"generate_requests_5h":           {Used: ptr(48), Remaining: ptr(48), Unit: "requests", Window: "5h"},
			"chat_requests_1d":               {Used: ptr(271), Remaining: ptr(271), Unit: "requests", Window: "1d"},
			"generate_requests_1d":           {Used: ptr(141), Remaining: ptr(141), Unit: "requests", Window: "1d"},
			"avg_latency_ms_today":           {Used: ptr(318), Remaining: ptr(318), Unit: "ms", Window: "today"},
			"avg_latency_ms_5h":              {Used: ptr(294), Remaining: ptr(294), Unit: "ms", Window: "5h"},
			"avg_latency_ms_1d":              {Used: ptr(321), Remaining: ptr(321), Unit: "ms", Window: "1d"},
			"http_4xx_today":                 {Used: ptr(3), Remaining: ptr(3), Unit: "responses", Window: "today"},
			"http_5xx_today":                 {Used: ptr(1), Remaining: ptr(1), Unit: "responses", Window: "today"},
			"model_llama3_1_8b_requests":     {Used: ptr(1290), Remaining: ptr(1290), Unit: "requests", Window: "all-time"},
			"model_qwen2_5_coder_requests":   {Used: ptr(740), Remaining: ptr(740), Unit: "requests", Window: "all-time"},
			"model_deepseek_r1_14b_requests": {Used: ptr(280), Remaining: ptr(280), Unit: "requests", Window: "all-time"},
			"model_llama3_1_8b_requests_today": {
				Used: ptr(236), Remaining: ptr(236), Unit: "requests", Window: "today",
			},
			"model_qwen2_5_coder_requests_today": {
				Used: ptr(131), Remaining: ptr(131), Unit: "requests", Window: "today",
			},
			"source_local_requests":       {Used: ptr(1980), Remaining: ptr(1980), Unit: "requests", Window: "all-time"},
			"source_cloud_requests":       {Used: ptr(330), Remaining: ptr(330), Unit: "requests", Window: "all-time"},
			"source_local_requests_today": {Used: ptr(362), Remaining: ptr(362), Unit: "requests", Window: "today"},
			"source_cloud_requests_today": {Used: ptr(50), Remaining: ptr(50), Unit: "requests", Window: "today"},
			"tool_read_file":              {Used: ptr(420), Remaining: ptr(420), Unit: "calls", Window: "all-time"},
			"tool_edit_file":              {Used: ptr(318), Remaining: ptr(318), Unit: "calls", Window: "all-time"},
			"tool_run_shell_command":      {Used: ptr(196), Remaining: ptr(196), Unit: "calls", Window: "all-time"},
			"tool_web_search":             {Used: ptr(82), Remaining: ptr(82), Unit: "calls", Window: "all-time"},
		},
		Resets: map[string]time.Time{
			"usage_five_hour": now.Add(2*time.Hour + 39*time.Minute),
			"usage_one_day":   now.Add(7*time.Hour + 18*time.Minute),
		},
		Attributes: map[string]string{
			"account_email":       "dev@acme-corp.io",
			"account_name":        "Acme Dev",
			"plan_name":           "pro",
			"selected_model":      "qwen2.5-coder:32b",
			"cloud_disabled":      "false",
			"cloud_source":        "desktop",
			"cli_version":         "0.11.4",
			"auth_type":           "api_key",
			"billing_cycle_start": now.Add(-9 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"billing_cycle_end":   now.Add(21 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"block_start":         now.Add(-2*time.Hour - 21*time.Minute).UTC().Format(time.RFC3339),
			"block_end":           now.Add(2*time.Hour + 39*time.Minute).UTC().Format(time.RFC3339),
		},
		Raw: map[string]string{
			"models_top":         "qwen2.5-coder:32b, llama3.1:8b, deepseek-r1:14b, qwen2.5:14b",
			"loaded_models":      "qwen2.5-coder:32b, llama3.1:8b, deepseek-r1:14b",
			"tool_usage":         "read_file=420, edit_file=318, run_shell_command=196, web_search=82",
			"desktop_db_path":    "~/.ollama/db.sqlite",
			"server_config_path": "~/.ollama/server.json",
		},
		DailySeries: map[string][]core.TimePoint{
			"requests":                  demoSeries(now, 284, 301, 317, 342, 368, 391, 412),
			"messages":                  demoSeries(now, 262, 280, 296, 321, 347, 365, 388),
			"sessions":                  demoSeries(now, 42, 44, 47, 51, 54, 58, 63),
			"tool_calls":                demoSeries(now, 91, 104, 113, 124, 136, 148, 157),
			"usage_model_llama3_1_8b":   demoSeries(now, 128, 143, 157, 176, 189, 207, 236),
			"usage_model_qwen2_5_coder": demoSeries(now, 72, 81, 95, 101, 113, 122, 131),
			"usage_source_local":        demoSeries(now, 251, 269, 286, 309, 331, 346, 362),
			"usage_source_cloud":        demoSeries(now, 33, 32, 31, 33, 37, 45, 50),
		},
		Message: "388 msgs today, 412 req today, 138 req 5h, 24 models",
	}

	// claude-code
	snaps["claude-code"] = core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"usage_five_hour":        {Used: ptr(38), Unit: "%", Window: "rolling-5h"},
			"usage_seven_day":        {Used: ptr(79), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_sonnet": {Used: ptr(63), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_opus":   {Used: ptr(84), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_cowork": {Used: ptr(16), Unit: "%", Window: "rolling-7d"},
			"5h_block_cost":          {Used: ptr(11.25), Unit: "USD", Window: "5h"},
			"5h_block_input":         {Used: ptr(9240), Unit: "tokens", Window: "5h"},
			"5h_block_cache_read_tokens": {
				Used: ptr(3120), Unit: "tokens", Window: "5h",
			},
			"5h_block_msgs":          {Used: ptr(40), Unit: "messages", Window: "5h"},
			"5h_block_output":        {Used: ptr(17570), Unit: "tokens", Window: "5h"},
			"7d_api_cost":            {Used: ptr(512.59), Unit: "USD", Window: "7d"},
			"7d_input_tokens":        {Used: ptr(239700), Unit: "tokens", Window: "7d"},
			"7d_cache_read_tokens":   {Used: ptr(62210), Unit: "tokens", Window: "7d"},
			"7d_cache_create_tokens": {Used: ptr(12840), Unit: "tokens", Window: "7d"},
			"7d_reasoning_tokens":    {Used: ptr(18320), Unit: "tokens", Window: "7d"},
			"7d_messages":            {Used: ptr(2428), Unit: "messages", Window: "7d"},
			"7d_output_tokens":       {Used: ptr(60600), Unit: "tokens", Window: "7d"},
			"all_time_api_cost":      {Used: ptr(512.59), Unit: "USD"},
			"burn_rate":              {Used: ptr(4.80), Unit: "USD/h"},
			"today_api_cost":         {Used: ptr(246.47), Unit: "USD"},
			"messages_today":         {Used: ptr(112), Unit: "messages", Window: "today"},
			"sessions_today":         {Used: ptr(17), Unit: "sessions", Window: "today"},
			"tool_calls_today":       {Used: ptr(31), Unit: "calls", Window: "today"},
			"7d_tool_calls":          {Used: ptr(274), Unit: "calls", Window: "7d"},
			"today_cache_create_1h_tokens": {
				Used: ptr(2650), Unit: "tokens", Window: "today",
			},
			"today_cache_create_5m_tokens": {
				Used: ptr(980), Unit: "tokens", Window: "today",
			},
			"today_web_search_requests": {Used: ptr(8), Unit: "requests", Window: "today"},
			"today_web_fetch_requests":  {Used: ptr(23), Unit: "requests", Window: "today"},
			"7d_web_search_requests":    {Used: ptr(39), Unit: "requests", Window: "7d"},
			"7d_web_fetch_requests":     {Used: ptr(119), Unit: "requests", Window: "7d"},
			"model_claude_sonnet_4_5_cost_usd": {
				Used: ptr(307.25), Unit: "USD", Window: "7d",
			},
			"model_claude_sonnet_4_5_input_tokens": {
				Used: ptr(145900), Unit: "tokens", Window: "7d",
			},
			"model_claude_sonnet_4_5_output_tokens": {
				Used: ptr(37800), Unit: "tokens", Window: "7d",
			},
			"model_claude_opus_4_6_cost_usd": {
				Used: ptr(161.84), Unit: "USD", Window: "7d",
			},
			"model_claude_opus_4_6_input_tokens": {
				Used: ptr(70800), Unit: "tokens", Window: "7d",
			},
			"model_claude_opus_4_6_output_tokens": {
				Used: ptr(17900), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_1_cost_usd": {
				Used: ptr(43.50), Unit: "USD", Window: "7d",
			},
			"model_claude_haiku_4_1_input_tokens": {
				Used: ptr(23000), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_1_output_tokens": {
				Used: ptr(4900), Unit: "tokens", Window: "7d",
			},
			"client_cli_input_tokens":     {Used: ptr(168900), Unit: "tokens", Window: "7d"},
			"client_cli_output_tokens":    {Used: ptr(42400), Unit: "tokens", Window: "7d"},
			"client_cli_cached_tokens":    {Used: ptr(53200), Unit: "tokens", Window: "7d"},
			"client_cli_reasoning_tokens": {Used: ptr(13300), Unit: "tokens", Window: "7d"},
			"client_cli_total_tokens":     {Used: ptr(277800), Unit: "tokens", Window: "7d"},
			"client_cli_sessions":         {Used: ptr(42), Unit: "sessions", Window: "7d"},
			"client_desktop_app_input_tokens": {
				Used: ptr(50900), Unit: "tokens", Window: "7d",
			},
			"client_desktop_app_output_tokens": {
				Used: ptr(13200), Unit: "tokens", Window: "7d",
			},
			"client_desktop_app_cached_tokens": {
				Used: ptr(9010), Unit: "tokens", Window: "7d",
			},
			"client_desktop_app_reasoning_tokens": {
				Used: ptr(5020), Unit: "tokens", Window: "7d",
			},
			"client_desktop_app_total_tokens": {Used: ptr(78130), Unit: "tokens", Window: "7d"},
			"client_desktop_app_sessions":     {Used: ptr(11), Unit: "sessions", Window: "7d"},
			"client_janekbaraniewski_total_tokens": {
				Used: ptr(97200), Unit: "tokens", Window: "7d",
			},
			"client_janekbaraniewski_sessions": {Used: ptr(8), Unit: "sessions", Window: "7d"},
			"client_perf_trading_s_total_tokens": {
				Used: ptr(73100), Unit: "tokens", Window: "7d",
			},
			"client_perf_trading_s_sessions": {Used: ptr(5), Unit: "sessions", Window: "7d"},
			"client_agentusage_total_tokens": {
				Used: ptr(55700), Unit: "tokens", Window: "7d",
			},
			"client_agentusage_sessions": {Used: ptr(10), Unit: "sessions", Window: "7d"},
			"client_kubesreai_total_tokens": {
				Used: ptr(35300), Unit: "tokens", Window: "7d",
			},
			"client_kubesreai_sessions": {Used: ptr(3), Unit: "sessions", Window: "7d"},
			"tool_read_calls":           {Used: ptr(1420), Unit: "calls", Window: "7d"},
			"tool_bash_calls":           {Used: ptr(870), Unit: "calls", Window: "7d"},
			"tool_edit_calls":           {Used: ptr(560), Unit: "calls", Window: "7d"},
			"tool_webfetch_calls":       {Used: ptr(330), Unit: "calls", Window: "7d"},
			"tool_websearch_calls":      {Used: ptr(96), Unit: "calls", Window: "7d"},
		},
		Resets: map[string]time.Time{
			"billing_block":   now.Add(3*time.Hour + 12*time.Minute),
			"usage_five_hour": now.Add(4*time.Hour + 48*time.Minute),
			"usage_seven_day": now.Add(5*24*time.Hour + 6*time.Hour),
		},
		Raw: map[string]string{
			"account_email":      "dev@acme-corp.io",
			"model_usage":        "claude-sonnet-4-5: 62%, claude-opus-4-6: 30%, claude-haiku-4-1: 8%",
			"model_usage_window": "7d",
			"model_count":        "3",
			"block_start":        now.Add(-2 * time.Hour).UTC().Format(time.RFC3339),
			"block_end":          now.Add(3 * time.Hour).UTC().Format(time.RFC3339),
			"cache_usage":        "read 62k, write 13k",
			"tool_usage":         "web_fetch: 119, web_search: 39",
			"tool_count":         "31",
			"client_usage":       "CLI 27%, Perf Trading S 21%, Agentusage 15%, Kubesreai 14%, Janekbaraniewski 23%",
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":                           demoSeries(now, 44, 61, 53, 72, 84, 89, 109),
			"requests":                       demoSeries(now, 288, 301, 336, 354, 382, 415, 441),
			"tokens_client_cli":              demoSeries(now, 21300, 24700, 25900, 28100, 29400, 31800, 34600),
			"tokens_client_desktop_app":      demoSeries(now, 6100, 7200, 8000, 8700, 8900, 9800, 11100),
			"tokens_client_janekbaraniewski": demoSeries(now, 10200, 11400, 12600, 13200, 14100, 15200, 16500),
			"tokens_client_perf_trading_s":   demoSeries(now, 7600, 8100, 8700, 9300, 10100, 10800, 11700),
			"tokens_client_agentusage":       demoSeries(now, 5200, 6100, 6800, 7200, 7900, 8600, 9400),
			"tokens_client_kubesreai":        demoSeries(now, 3600, 4000, 4300, 4700, 5100, 5600, 6000),
			"usage_model_claude-opus-4-6":    demoSeries(now, 15, 17, 19, 20, 22, 24, 26),
			"usage_model_claude-sonnet-4-5":  demoSeries(now, 23, 26, 29, 31, 33, 35, 37),
			"tokens_model_claude_sonnet_4":   demoSeries(now, 12700, 13800, 15100, 16400, 17300, 18200, 19500),
		},
		Message: "~$246.47 today · $4.80/h",
	}

	// codex-cli
	snaps["codex-cli"] = core.UsageSnapshot{
		ProviderID: "codex",
		AccountID:  "codex-cli",
		Timestamp:  now,
		Status:     core.StatusLimited,
		Metrics: map[string]core.Metric{
			"rate_limit_primary":               {Used: ptr(92), Limit: ptr(100), Remaining: ptr(8), Unit: "%", Window: "5h"},
			"rate_limit_secondary":             {Used: ptr(74), Limit: ptr(100), Remaining: ptr(26), Unit: "%", Window: "7d"},
			"rate_limit_code_review_primary":   {Used: ptr(61), Limit: ptr(100), Remaining: ptr(39), Unit: "%", Window: "5h"},
			"rate_limit_code_review_secondary": {Used: ptr(28), Limit: ptr(100), Remaining: ptr(72), Unit: "%", Window: "7d"},
			"context_window":                   {Used: ptr(43200), Limit: ptr(258400), Unit: "tokens"},
			"session_cached_tokens":            {Used: ptr(26400), Unit: "tokens", Window: "session"},
			"session_input_tokens":             {Used: ptr(43200), Unit: "tokens", Window: "session"},
			"session_output_tokens":            {Used: ptr(12100), Unit: "tokens", Window: "session"},
			"session_reasoning_tokens":         {Used: ptr(3880), Unit: "tokens", Window: "session"},
			"session_total_tokens":             {Used: ptr(85580), Unit: "tokens", Window: "session"},
			"model_gpt_5_codex_input_tokens":   {Used: ptr(61200), Unit: "tokens", Window: "session"},
			"model_gpt_5_codex_output_tokens":  {Used: ptr(17600), Unit: "tokens", Window: "session"},
			"model_gpt_5_codex_cost_usd":       {Used: ptr(9.72), Unit: "USD", Window: "session"},
			"model_gpt_5_3_codex_input_tokens": {Used: ptr(11800), Unit: "tokens", Window: "session"},
			"model_gpt_5_3_codex_output_tokens": {
				Used: ptr(4500), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_3_codex_cost_usd": {Used: ptr(3.40), Unit: "USD", Window: "session"},
			"model_gpt_5_1_codex_max_input_tokens": {
				Used: ptr(371100), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_max_output_tokens": {
				Used: ptr(111500), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_max_cached_tokens": {
				Used: ptr(117700), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_max_reasoning_tokens": {
				Used: ptr(37100), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_max_total_tokens": {
				Used: ptr(637400), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_mini_input_tokens": {
				Used: ptr(119500), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_mini_output_tokens": {
				Used: ptr(18500), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_mini_cached_tokens": {
				Used: ptr(119100), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_mini_reasoning_tokens": {
				Used: ptr(59200), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_1_codex_mini_total_tokens": {
				Used: ptr(317300), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_input_tokens": {
				Used: ptr(127000), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_output_tokens": {
				Used: ptr(17400), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_cached_tokens": {
				Used: ptr(17400), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_reasoning_tokens": {
				Used: ptr(180200), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_mini_input_tokens": {
				Used: ptr(59200), Unit: "tokens", Window: "session",
			},
			"model_gpt_5_2_codex_mini_output_tokens": {
				Used: ptr(12700), Unit: "tokens", Window: "session",
			},
			"client_cli_input_tokens":          {Used: ptr(59800), Unit: "tokens", Window: "session"},
			"client_cli_output_tokens":         {Used: ptr(16900), Unit: "tokens", Window: "session"},
			"client_cli_cached_tokens":         {Used: ptr(24400), Unit: "tokens", Window: "session"},
			"client_cli_reasoning_tokens":      {Used: ptr(3220), Unit: "tokens", Window: "session"},
			"client_cli_total_tokens":          {Used: ptr(104320), Unit: "tokens", Window: "session"},
			"client_cli_sessions":              {Used: ptr(7), Unit: "sessions", Window: "today"},
			"client_desktop_app_total_tokens":  {Used: ptr(21840), Unit: "tokens", Window: "session"},
			"client_desktop_app_input_tokens":  {Used: ptr(13200), Unit: "tokens", Window: "session"},
			"client_desktop_app_output_tokens": {Used: ptr(5200), Unit: "tokens", Window: "session"},
			"client_desktop_app_cached_tokens": {Used: ptr(2900), Unit: "tokens", Window: "session"},
			"client_desktop_app_sessions":      {Used: ptr(2), Unit: "sessions", Window: "today"},
			"client_ide_total_tokens":          {Used: ptr(119500), Unit: "tokens", Window: "session"},
			"client_ide_input_tokens":          {Used: ptr(79900), Unit: "tokens", Window: "session"},
			"client_ide_output_tokens":         {Used: ptr(26700), Unit: "tokens", Window: "session"},
			"client_ide_cached_tokens":         {Used: ptr(10400), Unit: "tokens", Window: "session"},
			"client_ide_reasoning_tokens":      {Used: ptr(25600), Unit: "tokens", Window: "session"},
			"client_ide_sessions":              {Used: ptr(39), Unit: "sessions", Window: "today"},
		},
		Resets: map[string]time.Time{
			"rate_limit_primary":             now.Add(41 * time.Minute),
			"rate_limit_secondary":           now.Add(6*24*time.Hour + 18*time.Hour),
			"rate_limit_code_review_primary": now.Add(58 * time.Minute),
		},
		Raw: map[string]string{
			"account_email": "dev@acme-corp.io",
			"model_usage":   "gpt-5.1-codex-max: 55%, gpt-5.1-codex-mini: 31%, gpt-5.2-codex: 11%, gpt-5.2-codex-mini: 3%",
			"client_usage":  "Desktop App 70%, CLI 25%, IDE 5%",
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_cli":         demoSeries(now, 6200, 8100, 9400, 10800, 12500, 14800, 17300),
			"tokens_client_desktop_app": demoSeries(now, 1200, 1900, 2200, 2600, 2900, 3300, 3600),
			"tokens_client_ide":         demoSeries(now, 5400, 6700, 7900, 9200, 10100, 11300, 12500),
		},
		Message: "Primary limit nearly exhausted; code review still available",
	}

	// copilot
	snaps["copilot"] = core.UsageSnapshot{
		ProviderID: "copilot",
		AccountID:  "copilot",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"chat_quota": {
				Limit: ptr(300), Remaining: ptr(182), Used: ptr(118),
				Unit: "messages", Window: "month",
			},
			"completions_quota": {
				Limit: ptr(300), Remaining: ptr(228), Used: ptr(72),
				Unit: "completions", Window: "month",
			},
			"premium_interactions_quota": {
				Limit: ptr(50), Remaining: ptr(27), Used: ptr(23),
				Unit: "requests", Window: "month",
			},
			"context_window": {
				Limit: ptr(128000), Used: ptr(22600), Remaining: ptr(105400),
				Unit: "tokens", Window: "session",
			},
			"messages_today":      {Used: ptr(46), Unit: "messages", Window: "today"},
			"7d_messages":         {Used: ptr(223), Unit: "messages", Window: "7d"},
			"sessions_today":      {Used: ptr(9), Unit: "sessions", Window: "today"},
			"tool_calls_today":    {Used: ptr(15), Unit: "calls", Window: "today"},
			"total_messages":      {Used: ptr(1820), Unit: "messages", Window: "all-time"},
			"tokens_today":        {Used: ptr(28400), Unit: "tokens", Window: "today"},
			"7d_tokens":           {Used: ptr(166500), Unit: "tokens", Window: "7d"},
			"cli_messages":        {Used: ptr(2021), Unit: "messages", Window: "all-time"},
			"cli_turns":           {Used: ptr(3040), Unit: "turns", Window: "all-time"},
			"cli_total_calls":     {Used: ptr(200), Unit: "calls", Window: "all-time"},
			"cli_sessions":        {Used: ptr(54), Unit: "sessions", Window: "all-time"},
			"cli_reasoning_chars": {Used: ptr(1110), Unit: "chars", Window: "7d"},
			"cli_response_chars":  {Used: ptr(204), Unit: "chars", Window: "7d"},
			"cli_tokens":          {Used: ptr(51300), Unit: "tokens", Window: "7d"},
			"cli_input_tokens":    {Used: ptr(11200), Unit: "tokens", Window: "today"},
			"org_acme_total_seats": {
				Used: ptr(56), Unit: "seats", Window: "current",
			},
			"org_acme_active_seats": {
				Used: ptr(44), Unit: "seats", Window: "current",
			},
			"model_gpt_5_mini_input_tokens": {
				Used: ptr(79200), Unit: "tokens", Window: "7d",
			},
			"model_gpt_5_mini_output_tokens": {
				Used: ptr(18300), Unit: "tokens", Window: "7d",
			},
			"model_gpt_5_mini_cost_usd": {Used: ptr(16.20), Unit: "USD", Window: "7d"},
			"model_claude_sonnet_4_6_input_tokens": {
				Used: ptr(33800), Unit: "tokens", Window: "7d",
			},
			"model_claude_sonnet_4_6_output_tokens": {
				Used: ptr(9100), Unit: "tokens", Window: "7d",
			},
			"model_claude_sonnet_4_6_cost_usd": {
				Used: ptr(9.86), Unit: "USD", Window: "7d",
			},
			"model_claude_haiku_4_5_input_tokens": {
				Used: ptr(33000), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_5_output_tokens": {
				Used: ptr(9200), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_5_messages": {
				Used: ptr(255), Unit: "messages", Window: "7d",
			},
			"model_claude_haiku_4_5_reasoning_chars": {
				Used: ptr(45), Unit: "chars", Window: "7d",
			},
			"model_claude_haiku_4_5_response_chars": {
				Used: ptr(42), Unit: "chars", Window: "7d",
			},
			"client_owner_repo_total_tokens": {Used: ptr(90400), Unit: "tokens", Window: "7d"},
			"client_owner_repo_input_tokens": {Used: ptr(74200), Unit: "tokens", Window: "7d"},
			"client_owner_repo_output_tokens": {
				Used: ptr(16200), Unit: "tokens", Window: "7d",
			},
			"client_owner_repo_sessions": {Used: ptr(14), Unit: "sessions", Window: "7d"},
			"client_vscode_total_tokens": {Used: ptr(22600), Unit: "tokens", Window: "7d"},
			"client_vscode_input_tokens": {Used: ptr(18800), Unit: "tokens", Window: "7d"},
			"client_vscode_output_tokens": {
				Used: ptr(3800), Unit: "tokens", Window: "7d",
			},
			"client_vscode_sessions":  {Used: ptr(5), Unit: "sessions", Window: "7d"},
			"client_cli_total_tokens": {Used: ptr(11900), Unit: "tokens", Window: "7d"},
			"client_cli_input_tokens": {Used: ptr(9800), Unit: "tokens", Window: "7d"},
			"client_cli_output_tokens": {
				Used: ptr(2100), Unit: "tokens", Window: "7d",
			},
			"client_cli_sessions": {Used: ptr(3), Unit: "sessions", Window: "7d"},
			"gh_core_rpm": {
				Limit: ptr(5000), Remaining: ptr(4795), Used: ptr(205),
				Unit: "requests", Window: "1h",
			},
			"gh_graphql_rpm": {
				Limit: ptr(5000), Remaining: ptr(5000), Used: ptr(0),
				Unit: "requests", Window: "1h",
			},
			"gh_search_rpm": {
				Limit: ptr(30), Remaining: ptr(30), Used: ptr(0),
				Unit: "requests", Window: "1h",
			},
		},
		Resets: map[string]time.Time{
			"gh_core_rpm_reset":    now.Add(23*time.Minute + 36*time.Second),
			"gh_graphql_rpm_reset": now.Add(59*time.Minute + 47*time.Second),
			"gh_search_rpm_reset":  now.Add(47 * time.Second),
			"quota_reset":          now.Add(24*24*time.Hour + 12*time.Hour),
		},
		Raw: map[string]string{
			"github_login":    "acme-dev",
			"access_type_sku": "business",
			"copilot_plan":    "business",
			"premium_interactions_quota_overage_permitted": "true",
			"model_usage":           "gpt-5-mini: 61%, claude-sonnet-4.6: 39%",
			"client_usage":          "owner/repo 72%, vscode 18%, cli 10%",
			"model_turns":           "gpt-5-mini: 730, claude-sonnet-4.6: 410",
			"model_sessions":        "gpt-5-mini: 28, claude-sonnet-4.6: 17",
			"model_tool_calls":      "gpt-5-mini: 112, claude-sonnet-4.6: 49",
			"model_response_chars":  "gpt-5-mini: 132k, claude-sonnet-4.6: 98k",
			"model_reasoning_chars": "gpt-5-mini: 24k, claude-sonnet-4.6: 17k",
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_owner_repo": demoSeries(now, 8300, 9200, 11100, 12400, 13800, 15700, 17700),
			"tokens_client_vscode":     demoSeries(now, 2100, 2400, 2900, 3300, 3800, 4100, 4600),
			"tokens_client_cli":        demoSeries(now, 900, 1200, 1400, 1700, 1800, 2200, 2500),
		},
		Message: "",
	}

	// cursor-ide
	snaps["cursor-ide"] = core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-ide",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used":      {Used: ptr(41), Unit: "%"},
			"plan_auto_percent_used": {Used: ptr(29), Unit: "%"},
			"plan_api_percent_used":  {Used: ptr(12), Unit: "%"},
			"plan_spend":             {Used: ptr(338), Limit: ptr(900), Unit: "USD"},
			"plan_total_spend_usd":   {Used: ptr(338), Unit: "USD"},
			"spend_limit":            {Used: ptr(338), Limit: ptr(3600), Remaining: ptr(3262), Unit: "USD"},
			"individual_spend":       {Used: ptr(234.73), Unit: "USD"},
			"chat_quota": {
				Limit: ptr(1000), Remaining: ptr(781), Used: ptr(219), Unit: "messages", Window: "month",
			},
			"completions_quota": {
				Limit: ptr(4000), Remaining: ptr(3010), Used: ptr(990), Unit: "requests", Window: "month",
			},
			"model_claude-4.5-opus-high-thinking_cost": {Used: ptr(1.81), Unit: "USD"},
			"model_claude-4.6-opus-high-thinking_cost": {Used: ptr(39.28), Unit: "USD"},
			"model_claude-4.6-opus-high-thinking_input_tokens": {
				Used: ptr(175500), Unit: "tokens", Window: "month",
			},
			"model_claude-4.6-opus-high-thinking_output_tokens": {
				Used: ptr(42100), Unit: "tokens", Window: "month",
			},
			"model_default_cost":                 {Used: ptr(0), Unit: "USD"},
			"model_gemini-3-flash_cost":          {Used: ptr(0.03), Unit: "USD"},
			"model_gemini-3-flash_input_tokens":  {Used: ptr(12400), Unit: "tokens", Window: "month"},
			"model_gemini-3-flash_output_tokens": {Used: ptr(4700), Unit: "tokens", Window: "month"},
			"plan_bonus":                         {Used: ptr(20.93), Unit: "USD"},
			"plan_included":                      {Used: ptr(20.00), Unit: "USD"},
			"requests_today":                     {Used: ptr(1400), Unit: "requests", Window: "today"},
			"total_ai_requests":                  {Used: ptr(59800), Unit: "requests", Window: "month"},
			"client_ide_sessions":                {Used: ptr(58500), Unit: "sessions", Window: "month"},
			"client_cli_agents_sessions":         {Used: ptr(1300), Unit: "sessions", Window: "month"},
			"source_ide_requests":                {Used: ptr(58500), Unit: "requests", Window: "month"},
			"source_cli_agents_requests":         {Used: ptr(1300), Unit: "requests", Window: "month"},
			"source_composer_requests":           {Used: ptr(6400), Unit: "requests", Window: "month"},
			"source_human_requests":              {Used: ptr(1100), Unit: "requests", Window: "month"},
			"composer_accepted_lines":            {Used: ptr(81), Unit: "lines", Window: "today"},
			"composer_suggested_lines":           {Used: ptr(81), Unit: "lines", Window: "today"},
		},
		Resets: map[string]time.Time{
			"billing_cycle_end": now.Add(16*24*time.Hour + 20*time.Hour),
		},
		Raw: map[string]string{
			"account_email":       "dev@acme-corp.io",
			"plan_name":           "pro",
			"team_membership":     "team",
			"billing_cycle_start": now.Add(-13 * 24 * time.Hour).UTC().Format(time.RFC3339),
			"billing_cycle_end":   now.Add(16*24*time.Hour + 20*time.Hour).UTC().Format(time.RFC3339),
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":                    demoSeries(now, 31, 39, 42, 46, 54, 58, 68),
			"requests":                demoSeries(now, 640, 701, 750, 811, 870, 919, 1006),
			"usage_source_ide":        demoSeries(now, 640, 710, 760, 820, 910, 980, 1040),
			"usage_source_cli_agents": demoSeries(now, 20, 30, 45, 60, 70, 80, 95),
			"usage_source_composer":   demoSeries(now, 50, 65, 74, 81, 93, 106, 112),
			"usage_model_claude-4.6-opus-high-thinking": demoSeries(now, 4200, 6100, 7200, 8100, 9300, 10100, 11600),
			"usage_model_gemini-3-flash":                demoSeries(now, 700, 930, 1180, 1340, 1620, 1840, 2050),
			"usage_model_claude-4.5-opus-high-thinking": demoSeries(now, 210, 260, 320, 410, 490, 560, 630),
		},
		Message: "Team — $338 / $3600 team spend ($3262 remaining)",
	}

	// gemini-cli
	snaps["gemini-cli"] = core.UsageSnapshot{
		ProviderID: "gemini_cli",
		AccountID:  "gemini-cli",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"quota":                  {Used: ptr(98), Limit: ptr(100), Unit: "%", Window: "month"},
			"quota_pro":              {Used: ptr(98), Limit: ptr(100), Unit: "%", Window: "month"},
			"quota_flash":            {Used: ptr(1), Limit: ptr(100), Unit: "%", Window: "month"},
			"quota_models_tracked":   {Used: ptr(6), Unit: "models", Window: "month"},
			"quota_models_low":       {Used: ptr(2), Unit: "models", Window: "month"},
			"quota_models_exhausted": {Used: ptr(0), Unit: "models", Window: "month"},
			"quota_model_gemini_2_5_pro_requests": {
				Used: ptr(98), Limit: ptr(100), Unit: "%", Window: "month",
			},
			"quota_model_gemini_2_0_flash_requests": {
				Used: ptr(1), Limit: ptr(100), Unit: "%", Window: "month",
			},
			"quota_model_gemini_2_5_flash_lite_requests": {
				Used: ptr(0.8), Limit: ptr(100), Unit: "%", Window: "month",
			},
			"total_conversations": {Used: ptr(184), Unit: "conversations", Window: "all-time"},
			"total_messages":      {Used: ptr(2480), Unit: "messages", Window: "all-time"},
			"total_sessions":      {Used: ptr(194), Unit: "sessions", Window: "all-time"},
			"total_turns":         {Used: ptr(3124), Unit: "turns", Window: "all-time"},
			"total_tool_calls":    {Used: ptr(618), Unit: "calls", Window: "all-time"},
			"messages_today":      {Used: ptr(37), Unit: "messages", Window: "today"},
			"sessions_today":      {Used: ptr(8), Unit: "sessions", Window: "today"},
			"tool_calls_today":    {Used: ptr(11), Unit: "calls", Window: "today"},
			"tokens_today":        {Used: ptr(31600), Unit: "tokens", Window: "today"},
			"today_input_tokens":  {Used: ptr(21400), Unit: "tokens", Window: "today"},
			"today_output_tokens": {Used: ptr(5100), Unit: "tokens", Window: "today"},
			"today_cached_tokens": {Used: ptr(5700), Unit: "tokens", Window: "today"},
			"today_reasoning_tokens": {
				Used: ptr(6800), Unit: "tokens", Window: "today",
			},
			"today_tool_tokens":   {Used: ptr(28100), Unit: "tokens", Window: "today"},
			"7d_messages":         {Used: ptr(226), Unit: "messages", Window: "7d"},
			"7d_sessions":         {Used: ptr(44), Unit: "sessions", Window: "7d"},
			"7d_tool_calls":       {Used: ptr(73), Unit: "calls", Window: "7d"},
			"7d_tokens":           {Used: ptr(170400), Unit: "tokens", Window: "7d"},
			"7d_input_tokens":     {Used: ptr(146700), Unit: "tokens", Window: "7d"},
			"7d_output_tokens":    {Used: ptr(23800), Unit: "tokens", Window: "7d"},
			"7d_cached_tokens":    {Used: ptr(33600), Unit: "tokens", Window: "7d"},
			"7d_reasoning_tokens": {Used: ptr(20600), Unit: "tokens", Window: "7d"},
			"7d_tool_tokens":      {Used: ptr(54100), Unit: "tokens", Window: "7d"},
			"client_cli_messages": {Used: ptr(1730), Unit: "messages", Window: "all-time"},
			"client_cli_turns":    {Used: ptr(2210), Unit: "turns", Window: "all-time"},
			"client_cli_tool_calls": {
				Used: ptr(489), Unit: "calls", Window: "all-time",
			},
			"client_cli_input_tokens":     {Used: ptr(94100), Unit: "tokens", Window: "7d"},
			"client_cli_output_tokens":    {Used: ptr(25100), Unit: "tokens", Window: "7d"},
			"client_cli_cached_tokens":    {Used: ptr(20600), Unit: "tokens", Window: "7d"},
			"client_cli_reasoning_tokens": {Used: ptr(7800), Unit: "tokens", Window: "7d"},
			"client_cli_total_tokens":     {Used: ptr(147600), Unit: "tokens", Window: "7d"},
			"client_cli_sessions":         {Used: ptr(29), Unit: "sessions", Window: "7d"},
			"model_gemini_3_pro_input_tokens": {
				Used: ptr(66900), Unit: "tokens", Window: "7d",
			},
			"model_gemini_3_pro_output_tokens": {
				Used: ptr(17800), Unit: "tokens", Window: "7d",
			},
			"model_gemini_3_flash_preview_input_tokens": {
				Used: ptr(36100), Unit: "tokens", Window: "7d",
			},
			"model_gemini_3_flash_preview_output_tokens": {
				Used: ptr(9300), Unit: "tokens", Window: "7d",
			},
			"tool_calls_success": {Used: ptr(185), Unit: "calls", Window: "7d"},
			"tool_google_web_search": {
				Used: ptr(48), Unit: "calls", Window: "7d",
			},
			"tool_run_shell_command": {
				Used: ptr(41), Unit: "calls", Window: "7d",
			},
			"tool_read_file": {Used: ptr(34), Unit: "calls", Window: "7d"},
		},
		Resets: map[string]time.Time{
			"quota_model_gemini_2_5_pro_requests_reset":        now.Add(22*time.Hour + 9*time.Minute),
			"quota_model_gemini_2_0_flash_requests_reset":      now.Add(7*time.Hour + 3*time.Minute),
			"quota_model_gemini_2_5_flash_lite_requests_reset": now.Add(7*time.Hour + 3*time.Minute),
			"quota_reset": now.Add(22*time.Hour + 9*time.Minute),
		},
		Raw: map[string]string{
			"oauth_status": "valid (refreshed)",
			"quota_api":    "ok (22 buckets)",
			"auth_type":    "oauth",
			"model_usage":  "gemini-3-pro-preview: 75%, gemini-3-flash-preview: 25%",
			"client_usage": "CLI 100%",
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_cli": demoSeries(now, 17100, 18300, 19800, 21200, 22600, 24300, 25100),
			"requests":          demoSeries(now, 29, 31, 36, 34, 40, 42, 45),
			"analytics_tokens":  demoSeries(now, 4.8e6, 5.0e6, 5.1e6, 5.3e6, 5.4e6, 5.5e6, 5.7e6),
		},
		Message: "",
	}

	// openrouter
	snaps["openrouter"] = core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: ptr(250.00), Remaining: ptr(193.74), Used: ptr(56.26), Unit: "USD", Window: "current",
			},
			"usage_daily":     {Used: ptr(8.92), Unit: "USD", Window: "1d"},
			"usage_weekly":    {Used: ptr(41.67), Unit: "USD", Window: "7d"},
			"usage_monthly":   {Used: ptr(98.11), Unit: "USD", Window: "30d"},
			"byok_daily":      {Used: ptr(1.14), Unit: "USD", Window: "1d"},
			"byok_weekly":     {Used: ptr(6.82), Unit: "USD", Window: "7d"},
			"byok_monthly":    {Used: ptr(17.44), Unit: "USD", Window: "30d"},
			"today_byok_cost": {Used: ptr(1.14), Unit: "USD", Window: "today"},
			"7d_byok_cost":    {Used: ptr(6.82), Unit: "USD", Window: "7d"},
			"30d_byok_cost":   {Used: ptr(17.44), Unit: "USD", Window: "30d"},
			"today_cost":      {Used: ptr(8.92), Unit: "USD", Window: "today"},
			"7d_api_cost":     {Used: ptr(41.67), Unit: "USD", Window: "7d"},
			"30d_api_cost":    {Used: ptr(98.11), Unit: "USD", Window: "30d"},
			"today_requests":  {Used: ptr(428), Unit: "requests", Window: "today"},
			"today_input_tokens": {
				Used: ptr(2601200), Unit: "tokens", Window: "today",
			},
			"today_output_tokens": {
				Used: ptr(359300), Unit: "tokens", Window: "today",
			},
			"today_reasoning_tokens": {Used: ptr(48200), Unit: "tokens", Window: "today"},
			"today_cached_tokens":    {Used: ptr(381000), Unit: "tokens", Window: "today"},
			"today_image_tokens":     {Used: ptr(9200), Unit: "tokens", Window: "today"},
			"today_media_prompts":    {Used: ptr(16), Unit: "count", Window: "today"},
			"today_audio_inputs":     {Used: ptr(9), Unit: "count", Window: "today"},
			"today_search_results":   {Used: ptr(43), Unit: "count", Window: "today"},
			"today_media_completions": {
				Used: ptr(11), Unit: "count", Window: "today",
			},
			"today_cancelled": {Used: ptr(4), Unit: "count", Window: "today"},
			"recent_requests": {Used: ptr(1840), Unit: "requests", Window: "recent"},
			"burn_rate":       {Used: ptr(1.87), Unit: "USD/h", Window: "1h"},
			"daily_projected": {Used: ptr(44.88), Unit: "USD", Window: "24h"},
			"limit_remaining": {Used: ptr(151.89), Unit: "USD", Window: "current"},
			"model_openai_gpt-4o-mini_cost_usd": {
				Used: ptr(19.42), Unit: "USD", Window: "activity",
			},
			"model_openai_gpt-4o-mini_input_tokens": {
				Used: ptr(1854000), Unit: "tokens", Window: "activity",
			},
			"model_openai_gpt-4o-mini_output_tokens": {
				Used: ptr(229300), Unit: "tokens", Window: "activity",
			},
			"model_anthropic_claude-3.5-sonnet_cost_usd": {
				Used: ptr(14.36), Unit: "USD", Window: "activity",
			},
			"model_anthropic_claude-3.5-sonnet_input_tokens": {
				Used: ptr(1118000), Unit: "tokens", Window: "activity",
			},
			"model_anthropic_claude-3.5-sonnet_output_tokens": {
				Used: ptr(143900), Unit: "tokens", Window: "activity",
			},
			"model_moonshotai_kimi-k2_cost_usd": {
				Used: ptr(9.41), Unit: "USD", Window: "activity",
			},
			"model_moonshotai_kimi-k2_input_tokens": {
				Used: ptr(920600), Unit: "tokens", Window: "activity",
			},
			"model_moonshotai_kimi-k2_output_tokens": {
				Used: ptr(120700), Unit: "tokens", Window: "activity",
			},
			"model_google_gemini-2.5-pro_cost_usd": {
				Used: ptr(6.84), Unit: "USD", Window: "activity",
			},
			"model_google_gemini-2.5-pro_input_tokens": {
				Used: ptr(684200), Unit: "tokens", Window: "activity",
			},
			"model_google_gemini-2.5-pro_output_tokens": {
				Used: ptr(78200), Unit: "tokens", Window: "activity",
			},
			"provider_openai_requests":    {Used: ptr(980), Unit: "requests", Window: "activity"},
			"provider_openai_cost_usd":    {Used: ptr(22.13), Unit: "USD", Window: "activity"},
			"provider_anthropic_requests": {Used: ptr(410), Unit: "requests", Window: "activity"},
			"provider_anthropic_cost_usd": {Used: ptr(13.02), Unit: "USD", Window: "activity"},
			"provider_openai_input_tokens": {
				Used: ptr(2100000), Unit: "tokens", Window: "activity",
			},
			"provider_openai_output_tokens": {
				Used: ptr(302000), Unit: "tokens", Window: "activity",
			},
			"provider_anthropic_input_tokens": {
				Used: ptr(1100000), Unit: "tokens", Window: "activity",
			},
			"provider_anthropic_output_tokens": {
				Used: ptr(141000), Unit: "tokens", Window: "activity",
			},
			"provider_moonshotai_requests": {
				Used: ptr(320), Unit: "requests", Window: "activity",
			},
			"provider_moonshotai_cost_usd": {
				Used: ptr(9.41), Unit: "USD", Window: "activity",
			},
			"provider_moonshotai_input_tokens": {
				Used: ptr(920600), Unit: "tokens", Window: "activity",
			},
			"provider_moonshotai_output_tokens": {
				Used: ptr(120700), Unit: "tokens", Window: "activity",
			},
			"provider_google_requests": {
				Used: ptr(210), Unit: "requests", Window: "activity",
			},
			"provider_google_cost_usd": {
				Used: ptr(6.84), Unit: "USD", Window: "activity",
			},
			"provider_google_input_tokens": {
				Used: ptr(684200), Unit: "tokens", Window: "activity",
			},
			"provider_google_output_tokens": {
				Used: ptr(78200), Unit: "tokens", Window: "activity",
			},
			"provider_novita_requests": {
				Used: ptr(21), Unit: "requests", Window: "activity",
			},
			"provider_novita_cost_usd": {
				Used: ptr(2.99), Unit: "USD", Window: "activity",
			},
			"provider_novita_input_tokens": {
				Used: ptr(520000), Unit: "tokens", Window: "activity",
			},
			"provider_novita_output_tokens": {
				Used: ptr(79000), Unit: "tokens", Window: "activity",
			},
			"provider_siliconflow_requests": {
				Used: ptr(7), Unit: "requests", Window: "activity",
			},
			"provider_siliconflow_cost_usd": {
				Used: ptr(0.38), Unit: "USD", Window: "activity",
			},
			"provider_siliconflow_input_tokens": {
				Used: ptr(118000), Unit: "tokens", Window: "activity",
			},
			"provider_siliconflow_output_tokens": {
				Used: ptr(15000), Unit: "tokens", Window: "activity",
			},
			"provider_deepinfra_requests": {
				Used: ptr(4), Unit: "requests", Window: "activity",
			},
			"provider_deepinfra_cost_usd": {
				Used: ptr(0.18), Unit: "USD", Window: "activity",
			},
			"provider_deepinfra_input_tokens": {
				Used: ptr(330000), Unit: "tokens", Window: "activity",
			},
			"provider_deepinfra_output_tokens": {
				Used: ptr(54000), Unit: "tokens", Window: "activity",
			},
			"analytics_7d_requests": {
				Used: ptr(40), Unit: "requests", Window: "7d",
			},
			"analytics_30d_requests": {
				Used: ptr(81), Unit: "requests", Window: "30d",
			},
			"analytics_7d_tokens": {
				Used: ptr(4.4e6), Unit: "tokens", Window: "7d",
			},
			"analytics_30d_tokens": {
				Used: ptr(8.7e6), Unit: "tokens", Window: "30d",
			},
			"analytics_7d_cost": {
				Used: ptr(8.35), Unit: "USD", Window: "7d",
			},
			"analytics_30d_cost": {
				Used: ptr(44.84), Unit: "USD", Window: "30d",
			},
			"analytics_active_days": {Used: ptr(26), Unit: "days", Window: "30d"},
			"analytics_models":      {Used: ptr(32), Unit: "models", Window: "30d"},
			"analytics_providers":   {Used: ptr(28), Unit: "providers", Window: "30d"},
			"analytics_endpoints":   {Used: ptr(6), Unit: "endpoints", Window: "30d"},
			"keys_total":            {Used: ptr(4), Unit: "keys", Window: "current"},
			"keys_active":           {Used: ptr(4), Unit: "keys", Window: "current"},
			"keys_disabled":         {Used: ptr(0), Unit: "keys", Window: "current"},
			"model_novita_moonshotai_kimi-k2_cost_usd": {
				Used: ptr(2.10), Unit: "USD", Window: "activity",
			},
			"model_novita_moonshotai_kimi-k2_input_tokens": {
				Used: ptr(4400000), Unit: "tokens", Window: "activity",
			},
			"model_novita_moonshotai_kimi-k2_output_tokens": {
				Used: ptr(510000), Unit: "tokens", Window: "activity",
			},
			"model_siliconflow_int4_cost_usd": {
				Used: ptr(0.38), Unit: "USD", Window: "activity",
			},
			"model_siliconflow_int4_input_tokens": {
				Used: ptr(1300000), Unit: "tokens", Window: "activity",
			},
			"model_siliconflow_int4_output_tokens": {
				Used: ptr(160000), Unit: "tokens", Window: "activity",
			},
			"model_unknown_cost_usd": {
				Used: ptr(0.09), Unit: "USD", Window: "activity",
			},
			"model_unknown_input_tokens": {
				Used: ptr(384200), Unit: "tokens", Window: "activity",
			},
			"model_unknown_output_tokens": {
				Used: ptr(47000), Unit: "tokens", Window: "activity",
			},
		},
		Raw: map[string]string{
			"activity_models":       "32",
			"is_management_key":     "false",
			"key_label":             "team-prod",
			"tier":                  "premium",
			"is_free_tier":          "false",
			"include_byok_in_limit": "true",
			"byok_in_use":           "true",
			"activity_providers":    "openai, anthropic, moonshotai, google, novita, siliconflow, deepinfra",
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":             demoSeries(now, 5.4, 6.1, 6.8, 7.2, 7.6, 8.2, 8.9),
			"requests":         demoSeries(now, 290, 312, 345, 358, 381, 402, 428),
			"analytics_tokens": demoSeries(now, 3.9e6, 4.0e6, 4.1e6, 4.2e6, 4.3e6, 4.35e6, 4.4e6),
		},
		Message: "$193.74 credits remaining",
	}

	// zen
	snaps["zen"] = core.UsageSnapshot{
		ProviderID: "zen",
		AccountID:  "zen",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"models_total":   {Used: ptr(216), Unit: "models", Window: "catalog"},
			"models_free":    {Used: ptr(31), Unit: "models", Window: "catalog"},
			"models_paid":    {Used: ptr(171), Unit: "models", Window: "catalog"},
			"models_unknown": {Used: ptr(14), Unit: "models", Window: "catalog"},
			"endpoint_chat_models": {
				Used: ptr(189), Unit: "models", Window: "catalog",
			},
			"endpoint_responses_models": {
				Used: ptr(204), Unit: "models", Window: "catalog",
			},
			"endpoint_messages_models": {Used: ptr(151), Unit: "models", Window: "catalog"},
			"endpoint_google_models":   {Used: ptr(27), Unit: "models", Window: "catalog"},
			"free_probe_input_tokens":  {Used: ptr(24), Unit: "tokens", Window: "last-probe"},
			"free_probe_output_tokens": {Used: ptr(11), Unit: "tokens", Window: "last-probe"},
			"free_probe_total_tokens":  {Used: ptr(35), Unit: "tokens", Window: "last-probe"},
			"free_probe_cached_tokens": {Used: ptr(9), Unit: "tokens", Window: "last-probe"},
			"free_probe_cost_usd":      {Used: ptr(0.0008), Unit: "USD", Window: "last-probe"},
			"billing_probe_input_tokens": {
				Used: ptr(19), Unit: "tokens", Window: "last-probe",
			},
			"billing_probe_output_tokens": {
				Used: ptr(7), Unit: "tokens", Window: "last-probe",
			},
			"billing_probe_total_tokens": {Used: ptr(26), Unit: "tokens", Window: "last-probe"},
			"billing_probe_cost_usd":     {Used: ptr(0.0014), Unit: "USD", Window: "last-probe"},
			"pricing_input_min_paid_per_1m": {
				Used: ptr(0.04), Unit: "USD/1Mtok", Window: "pricing",
			},
			"pricing_input_max_per_1m": {
				Used: ptr(15.00), Unit: "USD/1Mtok", Window: "pricing",
			},
			"pricing_output_min_paid_per_1m": {
				Used: ptr(0.08), Unit: "USD/1Mtok", Window: "pricing",
			},
			"pricing_output_max_per_1m": {
				Used: ptr(60.00), Unit: "USD/1Mtok", Window: "pricing",
			},
			"model_gpt_5_1_codex_mini_cost_usd": {
				Used: ptr(0.0026), Unit: "USD", Window: "last-probe",
			},
			"model_gpt_5_1_codex_mini_input_tokens": {
				Used: ptr(19), Unit: "tokens", Window: "last-probe",
			},
			"model_gpt_5_1_codex_mini_output_tokens": {
				Used: ptr(7), Unit: "tokens", Window: "last-probe",
			},
			"model_glm_5_free_cost_usd": {
				Used: ptr(0.0008), Unit: "USD", Window: "last-probe",
			},
			"model_glm_5_free_input_tokens": {
				Used: ptr(24), Unit: "tokens", Window: "last-probe",
			},
			"model_glm_5_free_output_tokens": {
				Used: ptr(11), Unit: "tokens", Window: "last-probe",
			},
			"free_probe_price_input_per_1m": {
				Used: ptr(0.30), Unit: "USD/1Mtok", Window: "pricing",
			},
			"free_probe_price_output_per_1m": {
				Used: ptr(0.60), Unit: "USD/1Mtok", Window: "pricing",
			},
			"billing_probe_price_input_per_1m": {
				Used: ptr(1.25), Unit: "USD/1Mtok", Window: "pricing",
			},
			"billing_probe_price_output_per_1m": {
				Used: ptr(5.00), Unit: "USD/1Mtok", Window: "pricing",
			},
			"subscription_active":            {Used: ptr(1), Unit: "flag", Window: "account"},
			"billing_payment_method_missing": {Used: ptr(0), Unit: "flag", Window: "account"},
			"billing_out_of_credits":         {Used: ptr(0), Unit: "flag", Window: "account"},
		},
		Raw: map[string]string{
			"workspace_id":             "wrk_9Xx4kL2aBc3",
			"subscription_status":      "active",
			"billing_status":           "active",
			"payment_required":         "false",
			"billing_url":              "https://opencode.ai/workspace/wrk_9Xx4kL2aBc3/billing",
			"team_billing_policy":      "charges_applied_to_workspace_owner",
			"team_model_access":        "role_based_admin_member",
			"models_count":             "216",
			"models_preview":           "gpt-5.1-codex-mini, claude-sonnet-4.6, gemini-3-pro, kimi-k2",
			"models_free_count":        "31",
			"models_paid_count":        "171",
			"models_unknown_count":     "14",
			"endpoint_unknown_models":  "0",
			"free_probe_status":        "200",
			"free_probe_endpoint":      "/chat/completions",
			"free_probe_model":         "glm-5-free",
			"free_probe_request_id":    "req_demo_free_01",
			"billing_probe_status":     "200",
			"billing_probe_endpoint":   "/responses",
			"billing_probe_model":      "gpt-5.1-codex-mini",
			"billing_probe_request_id": "req_demo_paid_02",
			"billing_probe_skipped":    "false",
			"provider_docs":            "https://opencode.ai/docs/zen/",
			"pricing_docs":             "https://opencode.ai/docs/zen/#pricing",
			"pricing_last_verified":    "2026-02-21",
			"billing_model":            "prepaid_payg",
			"billing_fee_policy":       "4.4% + $0.30 on card top-ups",
			"monthly_limits_scope":     "workspace_member_and_key",
			"subscription_mutability":  "billing_and_limits_can_change_over_time",
			"monthly_limits_supported": "true",
			"auto_reload_supported":    "true",
			"team_roles_supported":     "true",
			"byok_supported":           "true",
			"api_base_url":             "https://opencode.ai/zen/v1",
		},
		DailySeries: map[string][]core.TimePoint{
			"free_probe_tokens":    demoSeries(now, 29, 31, 33, 34, 35),
			"free_probe_cost":      demoSeries(now, 0.0006, 0.0007, 0.0007, 0.0008, 0.0008),
			"billing_probe_tokens": demoSeries(now, 21, 22, 24, 25, 26),
			"billing_probe_cost":   demoSeries(now, 0.0010, 0.0011, 0.0012, 0.0013, 0.0014),
		},
		Message: "Catalog: 216 models, probes healthy, subscription active",
	}

	addMissingDemoSnapshots(snaps, now)
	randomizeDemoSnapshots(snaps, now, rng)

	return snaps
}

func addMissingDemoSnapshots(snaps map[string]core.UsageSnapshot, now time.Time) {
	present := make(map[string]bool, len(snaps))
	for _, snap := range snaps {
		present[snap.ProviderID] = true
	}

	for _, provider := range providers.AllProviders() {
		providerID := provider.ID()
		if present[providerID] {
			continue
		}
		accountID := demoAccountID(providerID)
		snaps[accountID] = demoDefaultSnapshot(providerID, accountID, now)
		present[providerID] = true
	}
}

func demoAccountID(providerID string) string {
	switch providerID {
	case "claude_code":
		return "claude-code"
	case "codex":
		return "codex-cli"
	case "cursor":
		return "cursor-ide"
	case "gemini_cli":
		return "gemini-cli"
	case "openai":
		return "openai"
	case "anthropic":
		return "anthropic"
	case "openrouter":
		return "openrouter"
	case "groq":
		return "groq"
	case "mistral":
		return "mistral"
	case "deepseek":
		return "deepseek"
	case "xai":
		return "xai"
	case "gemini_api":
		return "gemini-api"
	default:
		return providerID
	}
}

func demoDefaultSnapshot(providerID, accountID string, now time.Time) core.UsageSnapshot {
	snap := core.UsageSnapshot{
		ProviderID: providerID,
		AccountID:  accountID,
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
		Message:    "Demo data",
	}

	switch providerID {
	case "openai":
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(10000), Remaining: ptr(7900), Unit: "requests", Window: "1m"}
		snap.Metrics["tpm"] = core.Metric{Limit: ptr(2000000), Remaining: ptr(1720000), Unit: "tokens", Window: "1m"}
		snap.Resets["rpm"] = now.Add(34 * time.Second)
		snap.Resets["tpm"] = now.Add(34 * time.Second)
		snap.Message = "OpenAI rate limits healthy"
	case "anthropic":
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(4000), Remaining: ptr(3550), Unit: "requests", Window: "1m"}
		snap.Metrics["tpm"] = core.Metric{Limit: ptr(400000), Remaining: ptr(312000), Unit: "tokens", Window: "1m"}
		snap.Resets["rpm"] = now.Add(22 * time.Second)
		snap.Resets["tpm"] = now.Add(22 * time.Second)
		snap.Message = "Anthropic limits and token budget available"
	case "openrouter":
		snap.Metrics["credit_balance"] = core.Metric{
			Limit: ptr(250.00), Remaining: ptr(193.74), Used: ptr(56.26), Unit: "USD", Window: "current",
		}
		snap.Metrics["usage_daily"] = core.Metric{Used: ptr(8.92), Unit: "USD", Window: "1d"}
		snap.Metrics["usage_weekly"] = core.Metric{Used: ptr(41.67), Unit: "USD", Window: "7d"}
		snap.Metrics["burn_rate"] = core.Metric{Used: ptr(1.87), Unit: "USD/h", Window: "current"}
		snap.Metrics["model_openai_gpt-4o-mini_cost_usd"] = core.Metric{Used: ptr(19.42), Unit: "USD", Window: "activity"}
		snap.Metrics["model_openai_gpt-4o-mini_input_tokens"] = core.Metric{Used: ptr(1854000), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_openai_gpt-4o-mini_output_tokens"] = core.Metric{Used: ptr(229300), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_anthropic_claude-3.5-sonnet_cost_usd"] = core.Metric{Used: ptr(14.36), Unit: "USD", Window: "activity"}
		snap.Metrics["model_anthropic_claude-3.5-sonnet_input_tokens"] = core.Metric{Used: ptr(1118000), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_anthropic_claude-3.5-sonnet_output_tokens"] = core.Metric{Used: ptr(143900), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_moonshotai_kimi-k2_cost_usd"] = core.Metric{Used: ptr(9.41), Unit: "USD", Window: "activity"}
		snap.Metrics["model_moonshotai_kimi-k2_input_tokens"] = core.Metric{Used: ptr(920600), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_moonshotai_kimi-k2_output_tokens"] = core.Metric{Used: ptr(120700), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_google_gemini-2.5-pro_cost_usd"] = core.Metric{Used: ptr(6.84), Unit: "USD", Window: "activity"}
		snap.Metrics["model_google_gemini-2.5-pro_input_tokens"] = core.Metric{Used: ptr(684200), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_google_gemini-2.5-pro_output_tokens"] = core.Metric{Used: ptr(78200), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_meta-llama_llama-3.1-70b_cost_usd"] = core.Metric{Used: ptr(3.51), Unit: "USD", Window: "activity"}
		snap.Metrics["model_meta-llama_llama-3.1-70b_input_tokens"] = core.Metric{Used: ptr(351000), Unit: "tokens", Window: "activity"}
		snap.Metrics["model_meta-llama_llama-3.1-70b_output_tokens"] = core.Metric{Used: ptr(41500), Unit: "tokens", Window: "activity"}
		snap.Raw["activity_models"] = "5"
		snap.Raw["is_management_key"] = "false"
		snap.Message = "$193.74 credits remaining"
	case "groq":
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(30000), Remaining: ptr(29240), Unit: "requests", Window: "1m"}
		snap.Metrics["tpm"] = core.Metric{Limit: ptr(900000), Remaining: ptr(812000), Unit: "tokens", Window: "1m"}
		snap.Metrics["rpd"] = core.Metric{Limit: ptr(500000), Remaining: ptr(486500), Unit: "requests", Window: "1d"}
		snap.Metrics["tpd"] = core.Metric{Limit: ptr(9000000), Remaining: ptr(8400000), Unit: "tokens", Window: "1d"}
		snap.Resets["rpm"] = now.Add(41 * time.Second)
		snap.Resets["rpd"] = now.Add(13*time.Hour + 18*time.Minute)
		snap.Message = "Remaining: 29240/30000 RPM, 486500/500000 RPD"
	case "mistral":
		snap.Metrics["monthly_budget"] = core.Metric{Limit: ptr(100.0), Unit: "EUR", Window: "1mo"}
		snap.Metrics["monthly_spend"] = core.Metric{
			Limit: ptr(100.0), Remaining: ptr(75.2), Used: ptr(24.8), Unit: "EUR", Window: "1mo",
		}
		snap.Metrics["credit_balance"] = core.Metric{Remaining: ptr(75.2), Unit: "EUR", Window: "current"}
		snap.Metrics["monthly_input_tokens"] = core.Metric{Used: ptr(1840000), Unit: "tokens", Window: "1mo"}
		snap.Metrics["monthly_output_tokens"] = core.Metric{Used: ptr(293000), Unit: "tokens", Window: "1mo"}
		snap.Raw["plan"] = "La Plateforme"
		snap.Message = "Mistral monthly spend: 24.8 EUR"
	case "deepseek":
		snap.Metrics["total_balance"] = core.Metric{Remaining: ptr(428.90), Unit: "CNY", Window: "current"}
		snap.Metrics["granted_balance"] = core.Metric{Remaining: ptr(100.00), Unit: "CNY", Window: "current"}
		snap.Metrics["topped_up_balance"] = core.Metric{Remaining: ptr(328.90), Unit: "CNY", Window: "current"}
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(6000), Remaining: ptr(5840), Unit: "requests", Window: "1m"}
		snap.Metrics["tpm"] = core.Metric{Limit: ptr(600000), Remaining: ptr(552000), Unit: "tokens", Window: "1m"}
		snap.Resets["rpm"] = now.Add(27 * time.Second)
		snap.Resets["tpm"] = now.Add(27 * time.Second)
		snap.Raw["currency"] = "CNY"
		snap.Message = "Balance: 428.90 CNY"
	case "xai":
		snap.Metrics["credits"] = core.Metric{
			Limit: ptr(500.00), Remaining: ptr(376.23), Used: ptr(123.77), Unit: "USD", Window: "current",
		}
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(6000), Remaining: ptr(5640), Unit: "requests", Window: "1m"}
		snap.Metrics["tpm"] = core.Metric{Limit: ptr(900000), Remaining: ptr(771000), Unit: "tokens", Window: "1m"}
		snap.Resets["rpm"] = now.Add(39 * time.Second)
		snap.Resets["tpm"] = now.Add(39 * time.Second)
		snap.Raw["api_key_name"] = "prod-key"
		snap.Message = "$376.23 remaining"
	case "gemini_api":
		snap.Metrics["available_models"] = core.Metric{Used: ptr(22), Unit: "models", Window: "current"}
		snap.Metrics["input_token_limit"] = core.Metric{Limit: ptr(1048576), Unit: "tokens", Window: "per-request"}
		snap.Metrics["output_token_limit"] = core.Metric{Limit: ptr(8192), Unit: "tokens", Window: "per-request"}
		snap.Metrics["rpm"] = core.Metric{Limit: ptr(60), Remaining: ptr(54), Unit: "requests", Window: "1m"}
		snap.Resets["rpm"] = now.Add(43 * time.Second)
		snap.Raw["models_sample"] = "gemini-2.5-flash, gemini-2.5-pro, gemini-2.0-flash"
		snap.Raw["total_models"] = "22"
		snap.Message = "auth OK; 22 models available"
	default:
		snap.Message = "Demo data available"
	}

	return snap
}

func randomizeDemoSnapshots(snaps map[string]core.UsageSnapshot, now time.Time, rng *rand.Rand) {
	for accountID, snap := range snaps {
		for key, metric := range snap.Metrics {
			snap.Metrics[key] = randomizeDemoMetric(metric, rng)
		}

		for key, resetAt := range snap.Resets {
			resetIn := resetAt.Sub(now)
			if resetIn <= 0 {
				continue
			}
			snap.Resets[key] = now.Add(jitterDuration(resetIn, 0.25, rng))
		}

		snap.Message = demoMessageForSnapshot(snap)
		snaps[accountID] = snap
	}
}

func randomizeDemoMetric(metric core.Metric, rng *rand.Rand) core.Metric {
	hasLimit := metric.Limit != nil && *metric.Limit > 0
	hasRemaining := metric.Remaining != nil
	hasUsed := metric.Used != nil

	if hasLimit && (hasRemaining || hasUsed) {
		limit := *metric.Limit
		used := limit * 0.5
		switch {
		case hasUsed:
			used = *metric.Used
		case hasRemaining:
			used = limit - *metric.Remaining
		}
		used = randomizeValue(used, 0.18, rng)
		if used < 0 {
			used = 0
		}
		if used > limit {
			used = limit
		}

		if hasUsed {
			updatedUsed := roundLike(*metric.Used, used)
			metric.Used = ptr(updatedUsed)
		}
		if hasRemaining {
			remaining := limit - used
			if remaining < 0 {
				remaining = 0
			}
			updatedRemaining := roundLike(*metric.Remaining, remaining)
			metric.Remaining = ptr(updatedRemaining)
		}
		return metric
	}

	if hasUsed {
		used := randomizeValue(*metric.Used, 0.2, rng)
		if used < 0 {
			used = 0
		}
		metric.Used = ptr(roundLike(*metric.Used, used))
	}
	if hasRemaining {
		remaining := randomizeValue(*metric.Remaining, 0.2, rng)
		if remaining < 0 {
			remaining = 0
		}
		metric.Remaining = ptr(roundLike(*metric.Remaining, remaining))
	}

	return metric
}

func randomizeValue(value, maxDelta float64, rng *rand.Rand) float64 {
	if value == 0 {
		return 0
	}
	factor := 1 + ((rng.Float64()*2 - 1) * maxDelta)
	return value * factor
}

func jitterDuration(base time.Duration, maxDelta float64, rng *rand.Rand) time.Duration {
	if base <= 0 {
		return base
	}
	factor := 1 + ((rng.Float64()*2 - 1) * maxDelta)
	jittered := time.Duration(float64(base) * factor)
	if jittered < 5*time.Second {
		return 5 * time.Second
	}
	return jittered
}

func roundLike(original, value float64) float64 {
	if math.Abs(original-math.Round(original)) < 1e-9 {
		return math.Round(value)
	}
	return math.Round(value*100) / 100
}

func demoSeries(now time.Time, values ...float64) []core.TimePoint {
	if len(values) == 0 {
		return nil
	}
	series := make([]core.TimePoint, 0, len(values))
	start := now.UTC().AddDate(0, 0, -(len(values) - 1))
	for i, value := range values {
		day := start.AddDate(0, 0, i)
		series = append(series, core.TimePoint{
			Date:  day.Format("2006-01-02"),
			Value: value,
		})
	}
	return series
}

func demoMessageForSnapshot(snap core.UsageSnapshot) string {
	switch snap.ProviderID {
	case "openai":
		if remaining, limit, ok := metricRemainingAndLimit(snap.Metrics, "rpm"); ok {
			return fmt.Sprintf("OpenAI healthy: %.0f/%.0f RPM remaining", remaining, limit)
		}
	case "anthropic":
		if remaining, limit, ok := metricRemainingAndLimit(snap.Metrics, "rpm"); ok {
			return fmt.Sprintf("Anthropic: %.0f/%.0f RPM remaining", remaining, limit)
		}
	case "openrouter":
		if remaining, ok := metricRemaining(snap.Metrics, "credit_balance"); ok {
			return fmt.Sprintf("$%.2f credits remaining", remaining)
		}
	case "groq":
		rpmRemaining, rpmLimit, rpmOK := metricRemainingAndLimit(snap.Metrics, "rpm")
		rpdRemaining, rpdLimit, rpdOK := metricRemainingAndLimit(snap.Metrics, "rpd")
		if rpmOK && rpdOK {
			return fmt.Sprintf("Remaining: %.0f/%.0f RPM, %.0f/%.0f RPD", rpmRemaining, rpmLimit, rpdRemaining, rpdLimit)
		}
	case "mistral":
		if spend, ok := metricUsed(snap.Metrics, "monthly_spend"); ok {
			return fmt.Sprintf("Mistral monthly spend: %.2f EUR", spend)
		}
	case "deepseek":
		if remaining, ok := metricRemaining(snap.Metrics, "total_balance"); ok {
			return fmt.Sprintf("Balance: %.2f CNY", remaining)
		}
	case "xai":
		if remaining, ok := metricRemaining(snap.Metrics, "credits"); ok {
			return fmt.Sprintf("$%.2f remaining", remaining)
		}
	case "cursor":
		spend, spendOK := metricUsed(snap.Metrics, "plan_spend")
		remaining, remainingOK := metricRemaining(snap.Metrics, "spend_limit")
		limit, limitOK := metricLimit(snap.Metrics, "spend_limit")
		if spendOK && remainingOK && limitOK {
			return fmt.Sprintf("Team — $%.2f / $%.0f team spend ($%.2f remaining)", spend, limit, remaining)
		}
	}

	return snap.Message
}

func metricUsed(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Used == nil {
		return 0, false
	}
	return *metric.Used, true
}

func metricLimit(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Limit == nil {
		return 0, false
	}
	return *metric.Limit, true
}

func metricRemaining(metrics map[string]core.Metric, key string) (float64, bool) {
	metric, ok := metrics[key]
	if !ok || metric.Remaining == nil {
		return 0, false
	}
	return *metric.Remaining, true
}

func metricRemainingAndLimit(metrics map[string]core.Metric, key string) (float64, float64, bool) {
	remaining, remainingOK := metricRemaining(metrics, key)
	limit, limitOK := metricLimit(metrics, key)
	if !remainingOK || !limitOK {
		return 0, 0, false
	}
	return remaining, limit, true
}
