package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/openusage/internal/config"
	"github.com/janekbaraniewski/openusage/internal/core"
	"github.com/janekbaraniewski/openusage/internal/providers"
	"github.com/janekbaraniewski/openusage/internal/tui"
)

func ptr(v float64) *float64 { return &v }

// demoProviderIDs lists the six providers shown in the demo dashboard.
var demoProviderIDs = map[string]bool{
	"gemini_cli":  true,
	"copilot":     true,
	"cursor":      true,
	"claude_code": true,
	"codex":       true,
	"openrouter":  true,
}

func main() {
	log.SetOutput(io.Discard)

	interval := 5 * time.Second
	accounts := buildDemoAccounts()
	demoProviders := buildDemoProviders(providers.AllProviders())

	providersByID := make(map[string]core.UsageProvider, len(demoProviders))
	for _, p := range demoProviders {
		providersByID[p.ID()] = p
	}

	model := tui.NewModel(
		0.20,
		0.05,
		false,
		config.DashboardConfig{},
		accounts,
		core.TimeWindow30d,
	)
	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithMouseCellMotion())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	refreshAll := func() {
		snaps := make(map[string]core.UsageSnapshot, len(accounts))
		for _, acct := range accounts {
			provider, ok := providersByID[acct.Provider]
			if !ok {
				continue
			}
			fetchCtx, fetchCancel := context.WithTimeout(ctx, 5*time.Second)
			snap, err := provider.Fetch(fetchCtx, acct)
			fetchCancel()
			if err != nil {
				snap = core.UsageSnapshot{
					ProviderID: acct.Provider,
					AccountID:  acct.ID,
					Timestamp:  time.Now(),
					Status:     core.StatusError,
					Message:    err.Error(),
				}
			}
			snaps[acct.ID] = snap
		}
		p.Send(tui.SnapshotsMsg(snaps))
	}

	go func() {
		refreshAll()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				refreshAll()
			}
		}
	}()

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
	accounts := make([]core.AccountConfig, 0, len(demoProviderIDs))
	seenAccountIDs := make(map[string]bool, len(demoProviderIDs))
	for _, provider := range providerList {
		if !demoProviderIDs[provider.ID()] {
			continue
		}
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
	return core.UsageSnapshot{
		ProviderID: p.base.ID(),
		AccountID:  acct.ID,
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics:    make(map[string]core.Metric),
		Resets:     make(map[string]time.Time),
		Raw:        make(map[string]string),
		Message:    "Demo data",
	}, nil
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

	// ── gemini-cli ──────────────────────────────────────────────────────
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
				Used: ptr(0.3), Limit: ptr(100), Unit: "%", Window: "month",
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

	// ── copilot ─────────────────────────────────────────────────────────
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
				Limit: ptr(128000), Used: ptr(95300), Remaining: ptr(32700),
				Unit: "tokens", Window: "session",
			},
			"messages_today":      {Used: ptr(15), Unit: "messages", Window: "today"},
			"7d_messages":         {Used: ptr(18), Unit: "messages", Window: "7d"},
			"sessions_today":      {Used: ptr(8), Unit: "sessions", Window: "today"},
			"tool_calls_today":    {Used: ptr(149), Unit: "calls", Window: "today"},
			"total_messages":      {Used: ptr(1820), Unit: "messages", Window: "all-time"},
			"tokens_today":        {Used: ptr(28400), Unit: "tokens", Window: "today"},
			"7d_tokens":           {Used: ptr(302900), Unit: "tokens", Window: "7d"},
			"cli_messages":        {Used: ptr(2021), Unit: "messages", Window: "all-time"},
			"cli_turns":           {Used: ptr(3040), Unit: "turns", Window: "all-time"},
			"cli_total_calls":     {Used: ptr(200), Unit: "calls", Window: "all-time"},
			"cli_sessions":        {Used: ptr(54), Unit: "sessions", Window: "all-time"},
			"cli_reasoning_chars": {Used: ptr(1110), Unit: "chars", Window: "7d"},
			"cli_response_chars":  {Used: ptr(204), Unit: "chars", Window: "7d"},
			"cli_tokens":          {Used: ptr(51300), Unit: "tokens", Window: "7d"},
			"cli_input_tokens":    {Used: ptr(11200), Unit: "tokens", Window: "today"},
			"org_demo_total_seats": {
				Used: ptr(56), Unit: "seats", Window: "current",
			},
			"org_demo_active_seats": {
				Used: ptr(44), Unit: "seats", Window: "current",
			},
			"model_claude_haiku_4_5_input_tokens": {
				Used: ptr(161200), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_5_output_tokens": {
				Used: ptr(57200), Unit: "tokens", Window: "7d",
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
			"model_claude_sonnet_4_6_input_tokens": {
				Used: ptr(50200), Unit: "tokens", Window: "7d",
			},
			"model_claude_sonnet_4_6_output_tokens": {
				Used: ptr(17200), Unit: "tokens", Window: "7d",
			},
			"model_claude_sonnet_4_6_cost_usd": {
				Used: ptr(9.86), Unit: "USD", Window: "7d",
			},
			"model_gpt_5_mini_input_tokens": {
				Used: ptr(12400), Unit: "tokens", Window: "7d",
			},
			"model_gpt_5_mini_output_tokens": {
				Used: ptr(4800), Unit: "tokens", Window: "7d",
			},
			"model_gpt_5_mini_cost_usd":       {Used: ptr(4.20), Unit: "USD", Window: "7d"},
			"client_skynet_labs_total_tokens": {Used: ptr(234900), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_input_tokens": {Used: ptr(188200), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_output_tokens": {
				Used: ptr(46700), Unit: "tokens", Window: "7d",
			},
			"client_skynet_labs_sessions":           {Used: ptr(7), Unit: "sessions", Window: "7d"},
			"client_skynet_labs_agent_total_tokens": {Used: ptr(51300), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_agent_input_tokens": {Used: ptr(42100), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_agent_output_tokens": {
				Used: ptr(9200), Unit: "tokens", Window: "7d",
			},
			"client_skynet_labs_agent_sessions": {Used: ptr(1), Unit: "sessions", Window: "7d"},
			"client_vscode_total_tokens":        {Used: ptr(16700), Unit: "tokens", Window: "7d"},
			"client_vscode_input_tokens":        {Used: ptr(13800), Unit: "tokens", Window: "7d"},
			"client_vscode_output_tokens": {
				Used: ptr(2900), Unit: "tokens", Window: "7d",
			},
			"client_vscode_sessions": {Used: ptr(1), Unit: "sessions", Window: "7d"},
			"gh_core_rpm": {
				Limit: ptr(5000), Remaining: ptr(4833), Used: ptr(167),
				Unit: "requests", Window: "1h",
			},
			"gh_graphql_rpm": {
				Limit: ptr(5000), Remaining: ptr(4989), Used: ptr(11),
				Unit: "requests", Window: "1h",
			},
			"gh_search_rpm": {
				Limit: ptr(30), Remaining: ptr(30), Used: ptr(0),
				Unit: "requests", Window: "1h",
			},
			"tool_bash_calls":      {Used: ptr(70), Unit: "calls", Window: "7d"},
			"tool_view_calls":      {Used: ptr(19), Unit: "calls", Window: "7d"},
			"tool_web_fetch_calls": {Used: ptr(19), Unit: "calls", Window: "7d"},
			"tool_edit_calls":      {Used: ptr(14), Unit: "calls", Window: "7d"},
			"tool_glob_calls":      {Used: ptr(11), Unit: "calls", Window: "7d"},
			"tool_grep_calls":      {Used: ptr(8), Unit: "calls", Window: "7d"},
			"tool_write_calls":     {Used: ptr(5), Unit: "calls", Window: "7d"},
			"tool_task_calls":      {Used: ptr(3), Unit: "calls", Window: "7d"},
		},
		Resets: map[string]time.Time{
			"gh_core_rpm_reset":    now.Add(27 * time.Minute),
			"gh_graphql_rpm_reset": now.Add(54 * time.Minute),
			"gh_search_rpm_reset":  now.Add(47 * time.Second),
			"quota_reset":          now.Add(21*24*time.Hour + 1*time.Hour),
		},
		Raw: map[string]string{
			"github_login":    "demo-user",
			"access_type_sku": "business",
			"copilot_plan":    "business",
			"premium_interactions_quota_overage_permitted": "true",
			"model_usage":      "claude-haiku-4-5: 72%, claude-sonnet-4.6: 22%, gpt-5-mini: 6%",
			"client_usage":     "skynet-labs 78%, skynet-labs-agent 17%, vscode 5%",
			"model_turns":      "claude-haiku-4-5: 730, claude-sonnet-4.6: 410, gpt-5-mini: 120",
			"model_sessions":   "claude-haiku-4-5: 28, claude-sonnet-4.6: 17, gpt-5-mini: 9",
			"model_tool_calls": "claude-haiku-4-5: 112, claude-sonnet-4.6: 49",
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_skynet_labs":       demoSeries(now, 8300, 9200, 11100, 12400, 13800, 15700, 17700),
			"tokens_client_skynet_labs_agent": demoSeries(now, 2100, 2400, 2900, 3300, 3800, 4100, 4600),
			"tokens_client_vscode":            demoSeries(now, 900, 1200, 1400, 1700, 1800, 2200, 2500),
		},
		Message: "",
	}

	// ── cursor-ide ──────────────────────────────────────────────────────
	snaps["cursor-ide"] = buildCursorDemoSnapshot(now)

	// ── claude-code ─────────────────────────────────────────────────────
	snaps["claude-code"] = core.UsageSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"usage_five_hour":        {Used: ptr(80), Unit: "%", Window: "rolling-5h"},
			"usage_seven_day":        {Used: ptr(12), Unit: "%", Window: "rolling-7d"},
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
			"burn_rate":              {Used: ptr(33.63), Unit: "USD/h"},
			"today_api_cost":         {Used: ptr(17.69), Unit: "USD"},
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
			"model_claude_opus_4_6_cost_usd": {
				Used: ptr(581.36), Unit: "USD", Window: "7d",
			},
			"model_claude_opus_4_6_input_tokens": {
				Used: ptr(131100), Unit: "tokens", Window: "7d",
			},
			"model_claude_opus_4_6_output_tokens": {
				Used: ptr(43000), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_5_20251001_cost_usd": {
				Used: ptr(11.41), Unit: "USD", Window: "7d",
			},
			"model_claude_haiku_4_5_20251001_input_tokens": {
				Used: ptr(23000), Unit: "tokens", Window: "7d",
			},
			"model_claude_haiku_4_5_20251001_output_tokens": {
				Used: ptr(14900), Unit: "tokens", Window: "7d",
			},
			// client: Skynet Labs (primary project)
			"client_skynet_labs_input_tokens":     {Used: ptr(62100000), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_output_tokens":    {Used: ptr(21700000), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_cached_tokens":    {Used: ptr(53200), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_reasoning_tokens": {Used: ptr(13300), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_total_tokens":     {Used: ptr(83800000), Unit: "tokens", Window: "7d"},
			"client_skynet_labs_sessions":         {Used: ptr(42), Unit: "sessions", Window: "7d"},
			"client_skynet_labs_requests":         {Used: ptr(890), Unit: "requests", Window: "7d"},
			// client: NovaTech Github Pages
			"client_novatech_github_io_input_tokens": {
				Used: ptr(50900), Unit: "tokens", Window: "7d",
			},
			"client_novatech_github_io_output_tokens": {
				Used: ptr(17900), Unit: "tokens", Window: "7d",
			},
			"client_novatech_github_io_total_tokens": {Used: ptr(68800), Unit: "tokens", Window: "7d"},
			"client_novatech_github_io_sessions":     {Used: ptr(11), Unit: "sessions", Window: "7d"},
			"client_novatech_github_io_requests":     {Used: ptr(681), Unit: "requests", Window: "7d"},
			// client: Harbor Trading Co
			"client_harbor_trading_co_total_tokens": {
				Used: ptr(67500), Unit: "tokens", Window: "7d",
			},
			"client_harbor_trading_co_sessions": {Used: ptr(8), Unit: "sessions", Window: "7d"},
			"client_harbor_trading_co_requests": {Used: ptr(773), Unit: "requests", Window: "7d"},
			// client: AgentPulse
			"client_agentpulse_total_tokens": {
				Used: ptr(51700000), Unit: "tokens", Window: "7d",
			},
			"client_agentpulse_sessions": {Used: ptr(5), Unit: "sessions", Window: "7d"},
			"client_agentpulse_requests": {Used: ptr(492), Unit: "requests", Window: "7d"},
			// Additional smaller clients
			"client_apex_analytics_total_tokens": {
				Used: ptr(35300), Unit: "tokens", Window: "7d",
			},
			"client_apex_analytics_sessions": {Used: ptr(3), Unit: "sessions", Window: "7d"},
			"client_apex_analytics_requests": {Used: ptr(310), Unit: "requests", Window: "7d"},
			"client_vortex_ml_total_tokens": {
				Used: ptr(28900), Unit: "tokens", Window: "7d",
			},
			"client_vortex_ml_sessions": {Used: ptr(4), Unit: "sessions", Window: "7d"},
			"client_vortex_ml_requests": {Used: ptr(245), Unit: "requests", Window: "7d"},
			// tools
			"tool_bash_calls":      {Used: ptr(306), Unit: "calls", Window: "7d"},
			"tool_read_calls":      {Used: ptr(232), Unit: "calls", Window: "7d"},
			"tool_edit_calls":      {Used: ptr(181), Unit: "calls", Window: "7d"},
			"tool_webfetch_calls":  {Used: ptr(74), Unit: "calls", Window: "7d"},
			"tool_websearch_calls": {Used: ptr(96), Unit: "calls", Window: "7d"},
		},
		Resets: map[string]time.Time{
			"billing_block":   now.Add(2*time.Hour + 29*time.Minute),
			"usage_five_hour": now.Add(2*time.Hour + 29*time.Minute),
			"usage_seven_day": now.Add(4*24*time.Hour + 11*time.Hour),
		},
		Raw: map[string]string{
			"account_email":      "demo.user@example.test",
			"model_usage":        "claude-opus-4-6: 98%, claude-haiku-4-5-20251001: 2%",
			"model_usage_window": "7d",
			"model_count":        "2",
			"block_start":        now.Add(-2*time.Hour - 31*time.Minute).UTC().Format(time.RFC3339),
			"block_end":          now.Add(2*time.Hour + 29*time.Minute).UTC().Format(time.RFC3339),
			"cache_usage":        "read 62k, write 13k",
			"tool_usage":         "web_fetch: 119, web_search: 39",
			"tool_count":         "31",
			"client_usage":       "Skynet Labs 24%, NovaTech Github IO 20%, Harbor Trading Co 20%, AgentPulse 15%, Apex Analytics 10%, Vortex ML 8%",
		},
		ModelUsage: []core.ModelUsageRecord{
			{
				RawModelID:       "claude-opus-4-6",
				Canonical:        "claude-opus-4-6",
				CanonicalFamily:  "claude",
				CanonicalVariant: "opus",
				CostUSD:          ptr(581.36),
				InputTokens:      ptr(131100),
				OutputTokens:     ptr(43000),
				CachedTokens:     ptr(62210),
				ReasoningTokens:  ptr(18320),
				Window:           "7d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "claude-haiku-4-5-20251001",
				Canonical:        "claude-haiku-4-5",
				CanonicalFamily:  "claude",
				CanonicalVariant: "haiku",
				CostUSD:          ptr(11.41),
				InputTokens:      ptr(23000),
				OutputTokens:     ptr(14900),
				Window:           "7d",
				Confidence:       1.0,
			},
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":                                  demoSeries(now, 44, 61, 53, 72, 84, 89, 109),
			"requests":                              demoSeries(now, 288, 301, 336, 354, 382, 415, 441),
			"tokens_client_skynet_labs":             demoSeries(now, 21300, 24700, 25900, 28100, 29400, 31800, 34600),
			"tokens_client_novatech_github_io":      demoSeries(now, 6100, 7200, 8000, 8700, 8900, 9800, 11100),
			"tokens_client_harbor_trading_co":       demoSeries(now, 10200, 11400, 12600, 13200, 14100, 15200, 16500),
			"tokens_client_agentpulse":              demoSeries(now, 7600, 8100, 8700, 9300, 10100, 10800, 11700),
			"tokens_client_apex_analytics":          demoSeries(now, 5200, 6100, 6800, 7200, 7900, 8600, 9400),
			"tokens_client_vortex_ml":               demoSeries(now, 3600, 4000, 4300, 4700, 5100, 5600, 6000),
			"usage_model_claude-opus-4-6":           demoSeries(now, 15, 17, 19, 20, 22, 24, 26),
			"usage_model_claude-haiku-4-5-20251001": demoSeries(now, 2, 3, 3, 4, 4, 5, 5),
		},
		Message: "~$17.69 today · $33.63/h",
	}

	// ── codex-cli ───────────────────────────────────────────────────────
	snaps["codex-cli"] = core.UsageSnapshot{
		ProviderID: "codex",
		AccountID:  "codex-cli",
		Timestamp:  now,
		Status:     core.StatusLimited,
		Metrics: map[string]core.Metric{
			"rate_limit_primary":               {Used: ptr(18), Limit: ptr(100), Remaining: ptr(82), Unit: "%", Window: "5h"},
			"rate_limit_secondary":             {Used: ptr(100), Limit: ptr(100), Remaining: ptr(0), Unit: "%", Window: "7d"},
			"rate_limit_code_review_primary":   {Used: ptr(2), Limit: ptr(100), Remaining: ptr(98), Unit: "%", Window: "7d"},
			"rate_limit_code_review_secondary": {Used: ptr(0), Limit: ptr(100), Remaining: ptr(100), Unit: "%", Window: "7d"},
			"plan_auto_percent_used":           {Used: ptr(18), Limit: ptr(100), Remaining: ptr(82), Unit: "%", Window: "5h"},
			"plan_api_percent_used":            {Used: ptr(100), Limit: ptr(100), Remaining: ptr(0), Unit: "%", Window: "7d"},
			"plan_percent_used":                {Used: ptr(100), Limit: ptr(100), Remaining: ptr(0), Unit: "%", Window: "7d"},
			"context_window":                   {Used: ptr(10400000), Limit: ptr(258400), Unit: "tokens"},
			"composer_context_pct":             {Used: ptr(100), Unit: "%", Window: "all-time"},
			"credit_balance":                   {Used: ptr(42.6), Unit: "USD", Window: "current"},
			"session_cached_tokens":            {Used: ptr(7320000), Unit: "tokens", Window: "session"},
			"session_input_tokens":             {Used: ptr(10800), Unit: "tokens", Window: "session"},
			"session_output_tokens":            {Used: ptr(121300), Unit: "tokens", Window: "session"},
			"session_reasoning_tokens":         {Used: ptr(40600), Unit: "tokens", Window: "session"},
			"session_total_tokens":             {Used: ptr(10500000), Unit: "tokens", Window: "session"},

			"model_gpt_5_3_codex_input_tokens":          {Used: ptr(1940000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_3_codex_output_tokens":         {Used: ptr(398000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_3_codex_cached_tokens":         {Used: ptr(60000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_3_codex_reasoning_tokens":      {Used: ptr(90000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_3_codex_total_tokens":          {Used: ptr(2388000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_max_input_tokens":      {Used: ptr(102000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_max_output_tokens":     {Used: ptr(15000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_max_cached_tokens":     {Used: ptr(12000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_max_reasoning_tokens":  {Used: ptr(4000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_max_total_tokens":      {Used: ptr(133000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_2_codex_input_tokens":          {Used: ptr(86000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_2_codex_output_tokens":         {Used: ptr(11000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_2_codex_cached_tokens":         {Used: ptr(2800), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_2_codex_reasoning_tokens":      {Used: ptr(900), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_2_codex_total_tokens":          {Used: ptr(100700), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_mini_input_tokens":     {Used: ptr(60000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_mini_output_tokens":    {Used: ptr(7000), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_mini_cached_tokens":    {Used: ptr(1500), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_mini_reasoning_tokens": {Used: ptr(300), Unit: "tokens", Window: "all-time"},
			"model_gpt_5_1_codex_mini_total_tokens":     {Used: ptr(68800), Unit: "tokens", Window: "all-time"},

			"client_cli_total_tokens":          {Used: ptr(6200000), Unit: "tokens", Window: "all-time"},
			"client_cli_input_tokens":          {Used: ptr(5100000), Unit: "tokens", Window: "all-time"},
			"client_cli_output_tokens":         {Used: ptr(980000), Unit: "tokens", Window: "all-time"},
			"client_cli_cached_tokens":         {Used: ptr(91000), Unit: "tokens", Window: "all-time"},
			"client_cli_reasoning_tokens":      {Used: ptr(29000), Unit: "tokens", Window: "all-time"},
			"client_cli_sessions":              {Used: ptr(44), Unit: "sessions", Window: "all-time"},
			"client_cli_requests":              {Used: ptr(5100), Unit: "requests", Window: "all-time"},
			"client_desktop_app_total_tokens":  {Used: ptr(3600000), Unit: "tokens", Window: "all-time"},
			"client_desktop_app_input_tokens":  {Used: ptr(2910000), Unit: "tokens", Window: "all-time"},
			"client_desktop_app_output_tokens": {Used: ptr(610000), Unit: "tokens", Window: "all-time"},
			"client_desktop_app_cached_tokens": {Used: ptr(62000), Unit: "tokens", Window: "all-time"},
			"client_desktop_app_sessions":      {Used: ptr(31), Unit: "sessions", Window: "all-time"},
			"client_desktop_app_requests":      {Used: ptr(3700), Unit: "requests", Window: "all-time"},
			"client_ide_total_tokens":          {Used: ptr(2100000), Unit: "tokens", Window: "all-time"},
			"client_ide_input_tokens":          {Used: ptr(1700000), Unit: "tokens", Window: "all-time"},
			"client_ide_output_tokens":         {Used: ptr(350000), Unit: "tokens", Window: "all-time"},
			"client_ide_cached_tokens":         {Used: ptr(40000), Unit: "tokens", Window: "all-time"},
			"client_ide_reasoning_tokens":      {Used: ptr(10000), Unit: "tokens", Window: "all-time"},
			"client_ide_sessions":              {Used: ptr(14), Unit: "sessions", Window: "all-time"},
			"client_ide_requests":              {Used: ptr(1400), Unit: "requests", Window: "all-time"},
			"client_cloud_agents_total_tokens": {Used: ptr(740000), Unit: "tokens", Window: "all-time"},
			"client_cloud_agents_requests":     {Used: ptr(620), Unit: "requests", Window: "all-time"},
			"client_cloud_agents_sessions":     {Used: ptr(6), Unit: "sessions", Window: "all-time"},

			"interface_cli_agents":   {Used: ptr(5100), Unit: "requests", Window: "all-time"},
			"interface_desktop_app":  {Used: ptr(3700), Unit: "requests", Window: "all-time"},
			"interface_ide":          {Used: ptr(1400), Unit: "requests", Window: "all-time"},
			"interface_cloud_agents": {Used: ptr(620), Unit: "requests", Window: "all-time"},

			"tool_exec_command":   {Used: ptr(10100), Unit: "calls", Window: "all-time"},
			"tool_apply_patch":    {Used: ptr(2300), Unit: "calls", Window: "all-time"},
			"tool_shell_command":  {Used: ptr(936), Unit: "calls", Window: "all-time"},
			"tool_web_search":     {Used: ptr(524), Unit: "calls", Window: "all-time"},
			"tool_write_stdin":    {Used: ptr(353), Unit: "calls", Window: "all-time"},
			"tool_update_plan":    {Used: ptr(51), Unit: "calls", Window: "all-time"},
			"tool_open":           {Used: ptr(19), Unit: "calls", Window: "all-time"},
			"tool_find":           {Used: ptr(17), Unit: "calls", Window: "all-time"},
			"tool_click":          {Used: ptr(13), Unit: "calls", Window: "all-time"},
			"tool_screenshot":     {Used: ptr(9), Unit: "calls", Window: "all-time"},
			"tool_image_query":    {Used: ptr(8), Unit: "calls", Window: "all-time"},
			"tool_go_test":        {Used: ptr(7), Unit: "calls", Window: "all-time"},
			"tool_gofmt":          {Used: ptr(6), Unit: "calls", Window: "all-time"},
			"tool_mcp_gopls":      {Used: ptr(4), Unit: "calls", Window: "all-time"},
			"tool_mcp_kubernetes": {Used: ptr(3), Unit: "calls", Window: "all-time"},
			"tool_mcp_linear":     {Used: ptr(2), Unit: "calls", Window: "all-time"},
			"tool_calls_total":    {Used: ptr(14352), Unit: "calls", Window: "all-time"},
			"tool_completed":      {Used: ptr(13060), Unit: "calls", Window: "all-time"},
			"tool_errored":        {Used: ptr(1060), Unit: "calls", Window: "all-time"},
			"tool_cancelled":      {Used: ptr(232), Unit: "calls", Window: "all-time"},
			"tool_success_rate":   {Used: ptr(91), Unit: "%", Window: "all-time"},

			"lang_go":        {Used: ptr(1300), Unit: "requests", Window: "all-time"},
			"lang_ts":        {Used: ptr(400), Unit: "requests", Window: "all-time"},
			"lang_shell":     {Used: ptr(153), Unit: "requests", Window: "all-time"},
			"lang_md":        {Used: ptr(148), Unit: "requests", Window: "all-time"},
			"lang_yaml":      {Used: ptr(76), Unit: "requests", Window: "all-time"},
			"lang_json":      {Used: ptr(23), Unit: "requests", Window: "all-time"},
			"lang_python":    {Used: ptr(18), Unit: "requests", Window: "all-time"},
			"lang_terraform": {Used: ptr(9), Unit: "requests", Window: "all-time"},

			"total_ai_requests":       {Used: ptr(10200), Unit: "requests", Window: "all-time"},
			"composer_requests":       {Used: ptr(10200), Unit: "requests", Window: "all-time"},
			"requests_today":          {Used: ptr(150), Unit: "requests", Window: "today"},
			"today_composer_requests": {Used: ptr(150), Unit: "requests", Window: "today"},
			"composer_sessions":       {Used: ptr(89), Unit: "sessions", Window: "all-time"},
			"total_prompts":           {Used: ptr(572), Unit: "prompts", Window: "all-time"},
			"composer_lines_added":    {Used: ptr(73300), Unit: "lines", Window: "all-time"},
			"composer_lines_removed":  {Used: ptr(17400), Unit: "lines", Window: "all-time"},
			"composer_files_changed":  {Used: ptr(639), Unit: "files", Window: "all-time"},
			"scored_commits":          {Used: ptr(14), Unit: "commits", Window: "all-time"},
			"ai_code_percentage":      {Used: ptr(22), Unit: "%", Window: "all-time"},
			"ai_deleted_files":        {Used: ptr(50), Unit: "files", Window: "all-time"},
			"ai_tracked_files":        {Used: ptr(639), Unit: "files", Window: "all-time"},
		},
		Resets: map[string]time.Time{
			"rate_limit_primary":               now.Add(4*time.Hour + 59*time.Minute),
			"rate_limit_secondary":             now.Add(3*24*time.Hour + 20*time.Hour),
			"rate_limit_code_review_primary":   now.Add(7 * 24 * time.Hour),
			"rate_limit_code_review_secondary": now.Add(7 * 24 * time.Hour),
		},
		Raw: map[string]string{
			"account_email":  "anon.codex.user@example.invalid",
			"account_id":     "anon-codex-team-01",
			"plan_type":      "team",
			"cli_version":    "0.105.0",
			"credit_balance": "$42.60",
			"model_usage":    "gpt-5-3-codex 94%, gpt-5-1-codex-max 5%, gpt-5-2-codex 1%, gpt-5-1-codex-mini 1%",
			"client_usage":   "CLI Agents 50%, Desktop App 36%, IDE 13%, Cloud Agents 6%",
			"tool_usage":     "exec_command 70%, apply_patch 16%, shell_command 7%, web_search 4%, write_stdin 2%, update_plan 0%",
			"language_usage": "go 61%, ts 19%, shell 7%, md 7%, yaml 4%, json 1%",
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_cli":              demoSeries(now, 1010000, 1040000, 1090000, 1120000, 1170000, 1220000, 1280000),
			"tokens_client_desktop_app":      demoSeries(now, 730000, 760000, 800000, 830000, 880000, 910000, 940000),
			"tokens_client_ide":              demoSeries(now, 240000, 260000, 280000, 310000, 330000, 350000, 370000),
			"usage_model_gpt_5_3_codex":      demoSeries(now, 305000, 321000, 332000, 346000, 362000, 380000, 395000),
			"usage_model_gpt_5_1_codex_max":  demoSeries(now, 12000, 14000, 16000, 17000, 19000, 22000, 23000),
			"usage_model_gpt_5_2_codex":      demoSeries(now, 9000, 9800, 10200, 10900, 11300, 11900, 12600),
			"usage_model_gpt_5_1_codex_mini": demoSeries(now, 6000, 7000, 7600, 8200, 9100, 9800, 10600),
			"usage_client_cli_agents":        demoSeries(now, 640, 690, 710, 760, 790, 840, 890),
			"usage_client_desktop_app":       demoSeries(now, 420, 450, 480, 520, 560, 600, 640),
			"usage_client_ide":               demoSeries(now, 180, 210, 230, 250, 280, 300, 320),
			"usage_client_cloud_agents":      demoSeries(now, 80, 85, 92, 98, 101, 110, 118),
			"usage_source_cli_agents":        demoSeries(now, 640, 690, 710, 760, 790, 840, 890),
			"usage_source_desktop_app":       demoSeries(now, 420, 450, 480, 520, 560, 600, 640),
			"usage_source_ide":               demoSeries(now, 180, 210, 230, 250, 280, 300, 320),
			"usage_source_cloud_agents":      demoSeries(now, 80, 85, 92, 98, 101, 110, 118),
			"analytics_tokens":               demoSeries(now, 1730000, 1820000, 1890000, 2010000, 2140000, 2280000, 2410000),
			"tokens_total":                   demoSeries(now, 1730000, 1820000, 1890000, 2010000, 2140000, 2280000, 2410000),
			"analytics_requests":             demoSeries(now, 1320, 1435, 1512, 1628, 1731, 1850, 1968),
			"requests":                       demoSeries(now, 1320, 1435, 1512, 1628, 1731, 1850, 1968),
		},
		Message: "Codex live usage + local session data",
	}

	// ── openrouter ──────────────────────────────────────────────────────
	snaps["openrouter"] = core.UsageSnapshot{
		ProviderID: "openrouter",
		AccountID:  "openrouter",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"credit_balance": {
				Limit: ptr(10.00), Remaining: ptr(1.72), Used: ptr(8.28), Unit: "USD", Window: "current",
			},
			"usage_daily":    {Used: ptr(0.18), Unit: "USD", Window: "1d"},
			"usage_weekly":   {Used: ptr(6.50), Unit: "USD", Window: "7d"},
			"usage_monthly":  {Used: ptr(8.28), Unit: "USD", Window: "30d"},
			"today_cost":     {Used: ptr(0.18), Unit: "USD", Window: "today"},
			"7d_api_cost":    {Used: ptr(6.50), Unit: "USD", Window: "7d"},
			"30d_api_cost":   {Used: ptr(8.28), Unit: "USD", Window: "30d"},
			"today_requests": {Used: ptr(21), Unit: "requests", Window: "today"},
			"today_input_tokens": {
				Used: ptr(746000), Unit: "tokens", Window: "today",
			},
			"today_output_tokens": {
				Used: ptr(89300), Unit: "tokens", Window: "today",
			},
			"today_reasoning_tokens": {Used: ptr(12200), Unit: "tokens", Window: "today"},
			"today_cached_tokens":    {Used: ptr(181000), Unit: "tokens", Window: "today"},
			"recent_requests":        {Used: ptr(464), Unit: "requests", Window: "recent"},
			"burn_rate":              {Used: ptr(0.04), Unit: "USD/h", Window: "1h"},
			"limit_remaining":        {Used: ptr(1.72), Unit: "USD", Window: "current"},
			"model_qwen_qwen3-coder-flash_cost_usd": {
				Used: ptr(2.44), Unit: "USD", Window: "activity",
			},
			"model_qwen_qwen3-coder-flash_input_tokens": {
				Used: ptr(7800000), Unit: "tokens", Window: "activity",
			},
			"model_qwen_qwen3-coder-flash_output_tokens": {
				Used: ptr(920000), Unit: "tokens", Window: "activity",
			},
			"model_moonshotai_kimi-k2-5_cost_usd": {
				Used: ptr(3.76), Unit: "USD", Window: "activity",
			},
			"model_moonshotai_kimi-k2-5_input_tokens": {
				Used: ptr(5800000), Unit: "tokens", Window: "activity",
			},
			"model_moonshotai_kimi-k2-5_output_tokens": {
				Used: ptr(720000), Unit: "tokens", Window: "activity",
			},
			"model_nvidia_nemotron-nano-9b-v2_cost_usd": {
				Used: ptr(0.02), Unit: "USD", Window: "activity",
			},
			"model_nvidia_nemotron-nano-9b-v2_input_tokens": {
				Used: ptr(381400), Unit: "tokens", Window: "activity",
			},
			"model_nvidia_nemotron-nano-9b-v2_output_tokens": {
				Used: ptr(42000), Unit: "tokens", Window: "activity",
			},
			"model_deepseek_deepseek-v3-2_cost_usd": {
				Used: ptr(0.04), Unit: "USD", Window: "activity",
			},
			"model_deepseek_deepseek-v3-2_input_tokens": {
				Used: ptr(127800), Unit: "tokens", Window: "activity",
			},
			"model_deepseek_deepseek-v3-2_output_tokens": {
				Used: ptr(18500), Unit: "tokens", Window: "activity",
			},
			"model_openai_gpt-o4s-r1203_cost_usd": {
				Used: ptr(0.01), Unit: "USD", Window: "activity",
			},
			"model_openai_gpt-o4s-r1203_input_tokens": {
				Used: ptr(94700), Unit: "tokens", Window: "activity",
			},
			"model_openai_gpt-o4s-r1203_output_tokens": {
				Used: ptr(11200), Unit: "tokens", Window: "activity",
			},
			"provider_openrouter_requests": {
				Used: ptr(464), Unit: "requests", Window: "activity",
			},
			"provider_openrouter_cost_usd": {
				Used: ptr(6.50), Unit: "USD", Window: "activity",
			},
			"provider_openrouter_input_tokens": {
				Used: ptr(14400000), Unit: "tokens", Window: "activity",
			},
			"provider_openrouter_output_tokens": {
				Used: ptr(1800000), Unit: "tokens", Window: "activity",
			},
			"analytics_7d_requests": {
				Used: ptr(464), Unit: "requests", Window: "7d",
			},
			"analytics_30d_requests": {
				Used: ptr(464), Unit: "requests", Window: "30d",
			},
			"analytics_7d_tokens": {
				Used: ptr(14.4e6), Unit: "tokens", Window: "7d",
			},
			"analytics_7d_cost": {
				Used: ptr(6.50), Unit: "USD", Window: "7d",
			},
			"analytics_30d_cost": {
				Used: ptr(8.28), Unit: "USD", Window: "30d",
			},
			"analytics_active_days": {Used: ptr(12), Unit: "days", Window: "30d"},
			"analytics_models":      {Used: ptr(24), Unit: "models", Window: "30d"},
			"analytics_providers":   {Used: ptr(1), Unit: "providers", Window: "30d"},
			"keys_total":            {Used: ptr(1), Unit: "keys", Window: "current"},
			"keys_active":           {Used: ptr(1), Unit: "keys", Window: "current"},
			// tools
			"tool_read_calls":      {Used: ptr(181), Unit: "calls", Window: "7d"},
			"tool_bash_calls":      {Used: ptr(108), Unit: "calls", Window: "7d"},
			"tool_edit_calls":      {Used: ptr(32), Unit: "calls", Window: "7d"},
			"tool_glob_calls":      {Used: ptr(23), Unit: "calls", Window: "7d"},
			"tool_grep_calls":      {Used: ptr(19), Unit: "calls", Window: "7d"},
			"tool_write_calls":     {Used: ptr(14), Unit: "calls", Window: "7d"},
			"tool_websearch_calls": {Used: ptr(11), Unit: "calls", Window: "7d"},
			"tool_webfetch_calls":  {Used: ptr(9), Unit: "calls", Window: "7d"},
			"tool_task_calls":      {Used: ptr(6), Unit: "calls", Window: "7d"},
		},
		Raw: map[string]string{
			"activity_models":       "24",
			"is_management_key":     "false",
			"key_label":             "demo-key",
			"tier":                  "premium",
			"is_free_tier":          "false",
			"include_byok_in_limit": "false",
			"byok_in_use":           "false",
			"activity_providers":    "openrouter",
		},
		ModelUsage: []core.ModelUsageRecord{
			{
				RawModelID:      "qwen/qwen3-coder-flash",
				Canonical:       "qwen3-coder-flash",
				CanonicalVendor: "qwen",
				CostUSD:         ptr(2.44),
				InputTokens:     ptr(7800000),
				OutputTokens:    ptr(920000),
				Window:          "activity",
				Confidence:      0.95,
			},
			{
				RawModelID:      "moonshotai/kimi-k2-5",
				Canonical:       "kimi-k2-5",
				CanonicalVendor: "moonshotai",
				CostUSD:         ptr(3.76),
				InputTokens:     ptr(5800000),
				OutputTokens:    ptr(720000),
				Window:          "activity",
				Confidence:      0.90,
			},
			{
				RawModelID:      "deepseek/deepseek-v3-2",
				Canonical:       "deepseek-v3-2",
				CanonicalVendor: "deepseek",
				CostUSD:         ptr(0.04),
				InputTokens:     ptr(127800),
				OutputTokens:    ptr(18500),
				Window:          "activity",
				Confidence:      0.95,
			},
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":             demoSeries(now, 0.42, 0.68, 0.91, 1.14, 1.37, 1.52, 1.72),
			"requests":         demoSeries(now, 38, 52, 71, 84, 96, 108, 121),
			"analytics_tokens": demoSeries(now, 1.8e6, 2.4e6, 3.1e6, 3.9e6, 4.5e6, 5.2e6, 5.8e6),
		},
		Message: "$1.72 credits remaining",
	}

	randomizeDemoSnapshots(snaps, now, rng)

	return snaps
}

func buildCursorDemoSnapshot(now time.Time) core.UsageSnapshot {
	metrics := map[string]core.Metric{
		// ── Gauges ───────────────────────────────────────────────────
		"team_budget":            {Used: ptr(531), Limit: ptr(3600), Remaining: ptr(3069), Unit: "USD"},
		"team_budget_self":       {Used: ptr(427), Unit: "USD"},
		"team_budget_others":     {Used: ptr(104), Unit: "USD"},
		"billing_cycle_progress": {Used: ptr(56.9), Limit: ptr(100), Unit: "%"},

		// ── Plan / Credits ──────────────────────────────────────────
		"plan_spend":           {Used: ptr(40.93), Limit: ptr(20.00), Unit: "USD"},
		"plan_total_spend_usd": {Used: ptr(40.93), Unit: "USD"},
		"spend_limit":          {Used: ptr(531.11), Limit: ptr(3600), Remaining: ptr(3068.89), Unit: "USD"},
		"individual_spend":     {Used: ptr(427.43), Unit: "USD"},
		"billing_total_cost":   {Used: ptr(41.12), Unit: "USD"},
		"today_cost":           {Used: ptr(5.23), Unit: "USD", Window: "today"},
		"plan_bonus":           {Used: ptr(20.93), Unit: "USD"},
		"plan_included":        {Used: ptr(20.00), Unit: "USD"},
		"plan_limit_usd":       {Used: ptr(3600), Unit: "USD"},
		"plan_included_amount": {Used: ptr(20.00), Unit: "USD"},

		// ── Usage percentages ───────────────────────────────────────
		"plan_percent_used":      {Used: ptr(100), Unit: "%"},
		"plan_auto_percent_used": {Used: ptr(0), Unit: "%"},
		"plan_api_percent_used":  {Used: ptr(100), Unit: "%"},
		"composer_context_pct":   {Used: ptr(43), Unit: "%"},

		// ── Team ────────────────────────────────────────────────────
		"team_size":   {Used: ptr(18), Unit: "members"},
		"team_owners": {Used: ptr(4), Unit: "owners"},

		// ── Activity ────────────────────────────────────────────────
		"requests_today":    {Used: ptr(15100), Unit: "requests", Window: "today"},
		"total_ai_requests": {Used: ptr(77800), Unit: "requests", Window: "all-time"},
		"composer_sessions": {Used: ptr(84), Unit: "sessions", Window: "all-time"},
		"composer_requests": {Used: ptr(645), Unit: "requests", Window: "all-time"},

		// ── Lines ───────────────────────────────────────────────────
		"composer_accepted_lines":  {Used: ptr(148), Unit: "lines", Window: "today"},
		"composer_suggested_lines": {Used: ptr(148), Unit: "lines", Window: "today"},
		"tab_accepted_lines":       {Used: ptr(0), Unit: "lines", Window: "today"},
		"tab_suggested_lines":      {Used: ptr(0), Unit: "lines", Window: "today"},

		// ── Billing tokens ──────────────────────────────────────────
		"billing_cached_tokens": {Used: ptr(63400000), Unit: "tokens", Window: "month"},
		"billing_input_tokens":  {Used: ptr(597100), Unit: "tokens", Window: "month"},
		"billing_output_tokens": {Used: ptr(320100), Unit: "tokens", Window: "month"},

		// ── AI tracking ─────────────────────────────────────────────
		"ai_deleted_files": {Used: ptr(21), Unit: "files", Window: "all-time"},
		"ai_tracked_files": {Used: ptr(16), Unit: "files", Window: "all-time"},

		// ── Models ──────────────────────────────────────────────────
		"model_claude-4.6-opus-high-thinking_cost":             {Used: ptr(39.28), Unit: "USD"},
		"model_claude-4.6-opus-high-thinking_input_tokens":     {Used: ptr(873600), Unit: "tokens", Window: "month"},
		"model_claude-4.6-opus-high-thinking_output_tokens":    {Used: ptr(47200), Unit: "tokens", Window: "month"},
		"model_gemini-3-flash_cost":                            {Used: ptr(0.03), Unit: "USD"},
		"model_gemini-3-flash_input_tokens":                    {Used: ptr(37400), Unit: "tokens", Window: "month"},
		"model_gemini-3-flash_output_tokens":                   {Used: ptr(4700), Unit: "tokens", Window: "month"},
		"model_claude-4.5-opus-high-thinking_cost":             {Used: ptr(1.81), Unit: "USD"},
		"model_claude-4.5-opus-high-thinking_input_tokens":     {Used: ptr(6200), Unit: "tokens", Window: "month"},
		"model_claude-4.5-opus-high-thinking_output_tokens":    {Used: ptr(1900), Unit: "tokens", Window: "month"},
		"model_claude-4-5-opus-high-thinking_cost":             {Used: ptr(307.54), Unit: "USD"},
		"model_claude-4-5-opus-high-thinking_input_tokens":     {Used: ptr(0), Unit: "tokens", Window: "month"},
		"model_claude-4-5-opus-high-thinking_output_tokens":    {Used: ptr(0), Unit: "tokens", Window: "month"},
		"model_claude-4.6-opus-high-thinking-v2_cost":          {Used: ptr(37.89), Unit: "USD"},
		"model_claude-4.6-opus-high-thinking-v2_input_tokens":  {Used: ptr(0), Unit: "tokens", Window: "month"},
		"model_claude-4.6-opus-high-thinking-v2_output_tokens": {Used: ptr(0), Unit: "tokens", Window: "month"},
		"model_gpt-5-mini_cost":                                {Used: ptr(2.12), Unit: "USD"},
		"model_gpt-5-mini_input_tokens":                        {Used: ptr(14200), Unit: "tokens", Window: "month"},
		"model_gpt-5-mini_output_tokens":                       {Used: ptr(3100), Unit: "tokens", Window: "month"},
		"model_deepseek-r2_cost":                               {Used: ptr(0.41), Unit: "USD"},
		"model_deepseek-r2_input_tokens":                       {Used: ptr(8100), Unit: "tokens", Window: "month"},
		"model_deepseek-r2_output_tokens":                      {Used: ptr(1200), Unit: "tokens", Window: "month"},
		"model_claude-4.5-sonnet_cost":                         {Used: ptr(0.18), Unit: "USD"},
		"model_claude-4.5-sonnet_input_tokens":                 {Used: ptr(3400), Unit: "tokens", Window: "month"},
		"model_claude-4.5-sonnet_output_tokens":                {Used: ptr(800), Unit: "tokens", Window: "month"},

		// ── Clients (interface breakdown) ────────────────────────────
		"interface_composer": {Used: ptr(67400), Unit: "requests", Window: "all-time"},
		"interface_cli":      {Used: ptr(10100), Unit: "requests", Window: "all-time"},
		"interface_human":    {Used: ptr(251), Unit: "requests", Window: "all-time"},
		"interface_tab":      {Used: ptr(97), Unit: "requests", Window: "all-time"},

		// ── Tool aggregates ─────────────────────────────────────────
		"tool_calls_total":  {Used: ptr(30400), Unit: "calls", Window: "all-time"},
		"tool_success_rate": {Used: ptr(95), Unit: "%", Window: "all-time"},
		"tool_completed":    {Used: ptr(28880), Unit: "calls", Window: "all-time"},
		"tool_errored":      {Used: ptr(1216), Unit: "calls", Window: "all-time"},
		"tool_cancelled":    {Used: ptr(304), Unit: "calls", Window: "all-time"},

		// ── Code statistics ─────────────────────────────────────────
		"composer_lines_added":   {Used: ptr(74600), Unit: "lines", Window: "all-time"},
		"composer_lines_removed": {Used: ptr(18500), Unit: "lines", Window: "all-time"},
		"composer_files_changed": {Used: ptr(844), Unit: "files", Window: "all-time"},
		"scored_commits":         {Used: ptr(239), Unit: "commits", Window: "all-time"},
		"ai_code_percentage":     {Used: ptr(98), Unit: "%", Window: "all-commits"},
		"total_prompts":          {Used: ptr(898), Unit: "prompts", Window: "all-time"},

		// ── Hidden aggregates (shown in compositions) ───────────────
		"agentic_sessions":       {Used: ptr(71), Unit: "sessions", Window: "all-time"},
		"non_agentic_sessions":   {Used: ptr(13), Unit: "sessions", Window: "all-time"},
		"composer_files_created": {Used: ptr(312), Unit: "files", Window: "all-time"},
		"composer_files_removed": {Used: ptr(47), Unit: "files", Window: "all-time"},
	}

	// ── Tool individual entries ──────────────────────────────────────
	toolEntries := []struct {
		name  string
		count float64
	}{
		{"run_terminal_command", 9000}, {"read_file", 6200}, {"run_terminal_cmd", 2800},
		{"search_replace", 2400}, {"edit_file", 1500}, {"write", 1200},
		{"list_dir", 980}, {"file_search", 870}, {"grep_search", 810},
		{"codebase_search", 740}, {"delete_file", 620}, {"insert_code", 580},
		{"replace_in_file", 530}, {"find_references", 490}, {"go_to_definition", 440},
		{"diagnostics", 410}, {"web_search", 380}, {"web_fetch", 350},
		{"ask_followup", 310}, {"execute_command", 290}, {"create_file", 270},
		{"rename_file", 240}, {"move_file", 210}, {"open_file", 190},
		{"get_file_content", 170}, {"apply_diff", 155}, {"revert_file", 140},
		{"git_diff", 128}, {"git_status", 115}, {"git_commit", 102},
		{"git_push", 95}, {"git_pull", 88}, {"git_log", 81},
		{"install_package", 74}, {"run_test", 68}, {"debug_session", 61},
		{"lint_file", 55}, {"format_code", 48}, {"refactor", 42},
		{"create_directory", 38}, {"copy_file", 35}, {"close_file", 31},
		{"get_diagnostics", 28}, {"code_action", 25}, {"hover_info", 22},
		{"completion_resolve", 20}, {"signature_help", 18}, {"document_symbol", 16},
		{"workspace_symbol", 14}, {"folding_range", 12}, {"selection_range", 11},
		{"semantic_tokens", 10}, {"inline_value", 9}, {"inlay_hint", 8},
		{"code_lens", 7}, {"document_link", 6}, {"color_info", 5},
		{"type_definition", 5}, {"declaration", 4}, {"implementation", 4},
		{"call_hierarchy", 3}, {"type_hierarchy", 3}, {"linked_editing", 3},
		{"moniker", 2}, {"notebook_cell", 2},
		{"mcp_github (mcp)", 45}, {"mcp_jira (mcp)", 38}, {"mcp_slack (mcp)", 32},
		{"mcp_confluence (mcp)", 28}, {"mcp_linear (mcp)", 24}, {"mcp_notion (mcp)", 20},
		{"mcp_figma (mcp)", 16}, {"mcp_sentry (mcp)", 14}, {"mcp_datadog (mcp)", 11},
		{"mcp_pagerduty (mcp)", 9}, {"mcp_vercel (mcp)", 7}, {"mcp_supabase (mcp)", 6},
		{"mcp_firebase (mcp)", 5}, {"mcp_stripe (mcp)", 4}, {"mcp_twilio (mcp)", 3},
		{"mcp_sendgrid (mcp)", 3}, {"mcp_cloudflare (mcp)", 2}, {"mcp_aws (mcp)", 2},
		{"mcp_azure (mcp)", 2}, {"mcp_gcp (mcp)", 2}, {"mcp_docker (mcp)", 2},
		{"mcp_k8s (mcp)", 1}, {"mcp_terraform (mcp)", 1}, {"mcp_vault (mcp)", 1},
		{"mcp_grafana (mcp)", 1}, {"mcp_prometheus (mcp)", 1}, {"mcp_elastic (mcp)", 1},
		{"mcp_redis (mcp)", 1}, {"mcp_postgres (mcp)", 1}, {"mcp_mongo (mcp)", 1},
		{"mcp_rabbit (mcp)", 1}, {"mcp_kafka (mcp)", 1}, {"mcp_nats (mcp)", 1},
	}
	for _, te := range toolEntries {
		metrics["tool_"+te.name] = core.Metric{Used: ptr(te.count), Unit: "calls", Window: "all-time"}
	}

	// ── Language entries ─────────────────────────────────────────────
	langEntries := []struct {
		name  string
		count float64
	}{
		{"go", 30400}, {"terraform", 12000}, {"shell", 5000},
		{"log", 1800}, {"txt", 1600}, {"tpl", 1400},
		{"md", 1200}, {"yaml", 1100}, {"json", 980},
		{"py", 870}, {"rs", 740}, {"ts", 680},
		{"js", 610}, {"css", 540}, {"html", 480},
		{"toml", 420}, {"sql", 370}, {"proto", 310},
		{"hcl", 270}, {"dockerfile", 230}, {"makefile", 190},
		{"xml", 160}, {"csv", 130}, {"ini", 100},
		{"conf", 80}, {"gitignore", 60},
	}
	for _, le := range langEntries {
		metrics["lang_"+le.name] = core.Metric{Used: ptr(le.count), Unit: "requests", Window: "all-time"}
	}

	billingStart := now.Add(-16 * 24 * time.Hour)
	billingEnd := now.Add(12*24*time.Hour + 2*time.Hour)

	return core.UsageSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-ide",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics:    metrics,
		Resets: map[string]time.Time{
			"billing_cycle_end": billingEnd,
		},
		Raw: map[string]string{
			"account_email":       "demo.user@acme-corp.dev",
			"plan_name":           "Team",
			"team_membership":     "team",
			"role":                "enterprise",
			"team_name":           "SELF_SERVE",
			"price":               "$40/mo",
			"billing_cycle_start": billingStart.UTC().Format(time.RFC3339),
			"billing_cycle_end":   billingEnd.UTC().Format(time.RFC3339),
		},
		DailySeries: map[string][]core.TimePoint{
			"cost":     demoSeries(now, 31, 39, 42, 46, 54, 58, 68),
			"requests": demoSeries(now, 640, 701, 750, 811, 870, 919, 1006),
			"usage_model_claude-4.6-opus-high-thinking":    demoSeries(now, 4200, 6100, 7200, 8100, 9300, 10100, 11600),
			"usage_model_gemini-3-flash":                   demoSeries(now, 700, 930, 1180, 1340, 1620, 1840, 2050),
			"usage_model_claude-4.5-opus-high-thinking":    demoSeries(now, 210, 260, 320, 410, 490, 560, 630),
			"usage_model_claude-4-5-opus-high-thinking":    demoSeries(now, 1200, 1800, 2100, 2500, 2800, 3200, 3600),
			"usage_model_claude-4.6-opus-high-thinking-v2": demoSeries(now, 380, 520, 610, 700, 810, 920, 1050),
		},
		Message: "Team — $531 / $3600 team spend ($3069 remaining)",
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
	case "openrouter":
		return "openrouter"
	case "copilot":
		return "copilot"
	default:
		return providerID
	}
}

func randomizeDemoSnapshots(snaps map[string]core.UsageSnapshot, now time.Time, rng *rand.Rand) {
	for accountID, snap := range snaps {
		for key, metric := range snap.Metrics {
			snap.Metrics[key] = randomizeDemoMetric(key, metric, rng)
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

func randomizeDemoMetric(key string, metric core.Metric, rng *rand.Rand) core.Metric {
	hasLimit := metric.Limit != nil && *metric.Limit > 0
	hasRemaining := metric.Remaining != nil
	hasUsed := metric.Used != nil

	if hasLimit && (hasRemaining || hasUsed) {
		limit := *metric.Limit
		used := limit * (0.12 + rng.Float64()*0.8)
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
		used := syntheticMetricValue(key, metric.Unit, rng)
		if used < 0 {
			used = 0
		}
		metric.Used = ptr(roundLike(*metric.Used, used))
	}
	if hasRemaining {
		remaining := syntheticMetricValue(key, metric.Unit, rng)
		if remaining < 0 {
			remaining = 0
		}
		metric.Remaining = ptr(roundLike(*metric.Remaining, remaining))
	}

	return metric
}

func syntheticMetricValue(key, unit string, rng *rand.Rand) float64 {
	lkey := strings.ToLower(key)
	lunit := strings.ToLower(unit)

	switch {
	case lunit == "flag":
		if rng.Float64() < 0.82 {
			return 0
		}
		return 1
	case strings.Contains(lkey, "price_") || strings.Contains(lunit, "/1mtok"):
		return 0.01 + rng.Float64()*25
	case strings.Contains(lkey, "cost") || strings.Contains(lunit, "usd") || strings.Contains(lunit, "eur") || strings.Contains(lunit, "cny"):
		return 0.1 + rng.Float64()*700
	case strings.Contains(lunit, "token") || strings.Contains(lunit, "char"):
		return 1000 + rng.Float64()*9_000_000
	case strings.Contains(lunit, "bytes"):
		return 5_000_000 + rng.Float64()*120_000_000_000
	case strings.Contains(lunit, "request") || strings.Contains(lunit, "message") || strings.Contains(lunit, "session") || strings.Contains(lunit, "call") || strings.Contains(lunit, "turn"):
		return 1 + rng.Float64()*6000
	case strings.Contains(lunit, "models"):
		return 1 + rng.Float64()*120
	case strings.Contains(lunit, "seats"):
		return 1 + rng.Float64()*80
	case strings.Contains(lunit, "ms"):
		return 20 + rng.Float64()*950
	case lunit == "%":
		return 1 + rng.Float64()*98
	case strings.Contains(lunit, "lines"):
		return 1 + rng.Float64()*500
	case strings.Contains(lunit, "days"):
		return 1 + rng.Float64()*31
	default:
		return 1 + rng.Float64()*5000
	}
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
	case "openrouter":
		if remaining, ok := metricRemaining(snap.Metrics, "credit_balance"); ok {
			return fmt.Sprintf("$%.2f credits remaining", remaining)
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
