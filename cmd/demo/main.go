package main

import (
	"fmt"
	"io"
	"log"
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

	model := tui.NewModel(
		0.20,
		0.05,
		false,
		config.DashboardConfig{},
		nil,
	)
	p := tea.NewProgram(model, tea.WithAltScreen())

	go func() {
		for {
			p.Send(tui.SnapshotsMsg(buildDemoSnapshots()))
			time.Sleep(5 * time.Second)
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}
}

func buildDemoSnapshots() map[string]core.QuotaSnapshot {
	now := time.Now()
	snaps := make(map[string]core.QuotaSnapshot)

	// ── claude-code ─────────────────────────────────────────────
	snaps["claude-code"] = core.QuotaSnapshot{
		ProviderID: "claude_code",
		AccountID:  "claude-code",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"usage_five_hour":        {Used: ptr(5), Unit: "%", Window: "rolling-5h"},
			"usage_seven_day":        {Used: ptr(79), Unit: "%", Window: "rolling-7d"},
			"usage_seven_day_sonnet": {Used: ptr(0), Unit: "%", Window: "rolling-7d"},
			"5h_block_cost":          {Used: ptr(11.25), Unit: "USD", Window: "5h"},
			"5h_block_input":         {Used: ptr(72), Unit: "tokens", Window: "5h"},
			"5h_block_msgs":          {Used: ptr(40), Unit: "messages", Window: "5h"},
			"5h_block_output":        {Used: ptr(1757), Unit: "tokens", Window: "5h"},
			"7d_api_cost":            {Used: ptr(512.59), Unit: "USD", Window: "7d"},
			"7d_input_tokens":        {Used: ptr(239700), Unit: "tokens", Window: "7d"},
			"7d_messages":            {Used: ptr(2428), Unit: "messages", Window: "7d"},
			"7d_output_tokens":       {Used: ptr(60600), Unit: "tokens", Window: "7d"},
			"all_time_api_cost":      {Used: ptr(512.59), Unit: "USD"},
			"burn_rate":              {Used: ptr(4.80), Unit: "USD/h"},
			"today_api_cost":         {Used: ptr(246.47), Unit: "USD"},
		},
		Resets: map[string]time.Time{
			"billing_block":   now.Add(3*time.Hour + 12*time.Minute),
			"usage_five_hour": now.Add(4*time.Hour + 48*time.Minute),
			"usage_seven_day": now.Add(5*24*time.Hour + 6*time.Hour),
		},
		Message: "~$246.47 today · $4.80/h",
	}

	// ── codex-cli ───────────────────────────────────────────────
	snaps["codex-cli"] = core.QuotaSnapshot{
		ProviderID: "codex",
		AccountID:  "codex-cli",
		Timestamp:  now,
		Status:     core.StatusLimited,
		Metrics: map[string]core.Metric{
			"rate_limit_primary":    {Used: ptr(0), Limit: ptr(100), Remaining: ptr(100), Unit: "%", Window: "5h"},
			"rate_limit_secondary":  {Used: ptr(100), Limit: ptr(100), Remaining: ptr(0), Unit: "%", Window: "7d"},
			"context_window":        {Used: ptr(43200), Limit: ptr(258400), Unit: "tokens"},
			"session_cached_tokens": {Used: ptr(26400), Unit: "tokens", Window: "session"},
			"session_input_tokens":  {Used: ptr(43200), Unit: "tokens", Window: "session"},
			"session_output_tokens": {Used: ptr(123), Unit: "tokens", Window: "session"},
			"session_total_tokens":  {Used: ptr(43300), Unit: "tokens", Window: "session"},
		},
		Resets: map[string]time.Time{
			"rate_limit_primary":   now.Add(4*time.Hour + 22*time.Minute),
			"rate_limit_secondary": now.Add(6*24*time.Hour + 18*time.Hour),
		},
		Raw: map[string]string{
			"account_email": "dev@acme-corp.io",
			"plan_type":     "team",
			"cli_version":   "0.104.0",
		},
		Message: "Codex CLI session data",
	}

	// ── copilot ─────────────────────────────────────────────────
	snaps["copilot"] = core.QuotaSnapshot{
		ProviderID: "copilot",
		AccountID:  "copilot",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"chat_quota": {
				Limit: ptr(300), Remaining: ptr(282), Used: ptr(18),
				Unit: "messages", Window: "month",
			},
			"completions_quota": {
				Limit: ptr(300), Remaining: ptr(300), Used: ptr(0),
				Unit: "completions", Window: "month",
			},
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
			"account_email":   "dev@acme-corp.io",
			"plan_name":       "Copilot (acme-dev)",
			"membership_type": "Free",
			"access_type_sku": "free",
			"copilot_plan":    "free",
		},
		Message: "Copilot (acme-dev) · Free",
	}

	// ── cursor-ide ──────────────────────────────────────────────
	snaps["cursor-ide"] = core.QuotaSnapshot{
		ProviderID: "cursor",
		AccountID:  "cursor-ide",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			"plan_percent_used":    {Used: ptr(100), Unit: "%"},
			"plan_spend":           {Used: ptr(338), Limit: ptr(338), Unit: "USD"},
			"plan_total_spend_usd": {Used: ptr(338), Unit: "USD"},
			"spend_limit":          {Used: ptr(338), Limit: ptr(3600), Remaining: ptr(3262), Unit: "USD"},
			"individual_spend":     {Used: ptr(234.73), Unit: "USD"},
			"model_claude-4.5-opus-high-thinking_cost": {Used: ptr(1.81), Unit: "USD"},
			"model_claude-4.6-opus-high-thinking_cost": {Used: ptr(39.28), Unit: "USD"},
			"model_default_cost":                       {Used: ptr(0), Unit: "USD"},
			"model_gemini-3-flash_cost":                {Used: ptr(0.03), Unit: "USD"},
			"plan_bonus":                               {Used: ptr(20.93), Unit: "USD"},
			"plan_included":                            {Used: ptr(20.00), Unit: "USD"},
		},
		Raw: map[string]string{
			"account_email":   "dev@acme-corp.io",
			"plan_name":       "pro",
			"team_membership": "team",
		},
		Message: "Team — $338 / $3600 team spend ($3262 remaining)",
	}

	// ── gemini-cli ──────────────────────────────────────────────
	// Gemini CLI emits metric keys as "<modelID>_<tokenType>" (no rate_limit_ prefix)
	// with Unit = tokenType (e.g. "RPM") and Limit/Remaining in percentages.
	geminiModels := []string{
		"gemini-2.0-flash-001",
		"gemini-2.0-flash-exp",
		"gemini-2.5-flash-001",
		"gemini-2.5-flash-exp",
		"gemini-2.5-flash-preview-04-17",
		"gemini-2.5-pro-001",
		"gemini-2.5-pro-exp-03-25",
		"gemini-3-flash-001",
		"gemini-3-flash-exp",
		"gemini-3-pro-001",
		"gemini-3-pro-exp",
	}
	geminiMetrics := make(map[string]core.Metric, len(geminiModels)+1)
	geminiResets := make(map[string]time.Time, len(geminiModels))
	for _, model := range geminiModels {
		key := model + "_RPM"
		limit := float64(100)
		remaining := float64(100)
		geminiMetrics[key] = core.Metric{
			Limit:     &limit,
			Remaining: &remaining,
			Unit:      "RPM",
			Window:    "~1 day",
		}
		geminiResets[key] = now.Add(23*time.Hour + 42*time.Minute)
	}
	convCount := float64(54)
	geminiMetrics["total_conversations"] = core.Metric{
		Used:   &convCount,
		Unit:   "conversations",
		Window: "all-time",
	}

	snaps["gemini-cli"] = core.QuotaSnapshot{
		ProviderID: "gemini_cli",
		AccountID:  "gemini-cli",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics:    geminiMetrics,
		Resets:     geminiResets,
		Raw: map[string]string{
			"account_email": "dev@acme-corp.io",
			"oauth_status":  "valid (refreshed)",
			"quota_api":     "ok (22 buckets)",
		},
		Message: "Gemini CLI (dev@acme-corp.io)",
	}

	addMissingDemoSnapshots(snaps, now)

	return snaps
}

func addMissingDemoSnapshots(snaps map[string]core.QuotaSnapshot, now time.Time) {
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

func demoDefaultSnapshot(providerID, accountID string, now time.Time) core.QuotaSnapshot {
	snap := core.QuotaSnapshot{
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
		snap.Raw["activity_models"] = "9"
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
