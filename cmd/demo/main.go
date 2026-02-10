package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/janekbaraniewski/agentusage/internal/core"
	"github.com/janekbaraniewski/agentusage/internal/tui"
)

func ptr(v float64) *float64 { return &v }

func main() {
	log.SetOutput(io.Discard)

	model := tui.NewModel(0.20, 0.05, false)
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

	// ── copilot-auto ────────────────────────────────────────────
	snaps["copilot-auto"] = core.QuotaSnapshot{
		ProviderID: "copilot",
		AccountID:  "copilot-auto",
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

	// ── gemini-cli-auto ─────────────────────────────────────────
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

	snaps["gemini-cli-auto"] = core.QuotaSnapshot{
		ProviderID: "gemini_cli",
		AccountID:  "gemini-cli-auto",
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

	return snaps
}
