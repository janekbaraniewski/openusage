package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildCursorDemoSnapshot(now time.Time) core.UsageSnapshot {
	metrics := map[string]core.Metric{
		"team_budget":            {Used: ptr(531), Limit: ptr(3600), Remaining: ptr(3069), Unit: "USD"},
		"team_budget_self":       {Used: ptr(427), Unit: "USD"},
		"team_budget_others":     {Used: ptr(104), Unit: "USD"},
		"billing_cycle_progress": {Used: ptr(56.9), Limit: ptr(100), Unit: "%"},

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

		"plan_percent_used":      {Used: ptr(100), Unit: "%"},
		"plan_auto_percent_used": {Used: ptr(0), Unit: "%"},
		"plan_api_percent_used":  {Used: ptr(100), Unit: "%"},
		"composer_context_pct":   {Used: ptr(43), Unit: "%"},

		"team_size":   {Used: ptr(18), Unit: "members"},
		"team_owners": {Used: ptr(4), Unit: "owners"},

		"requests_today":    {Used: ptr(15100), Unit: "requests", Window: "today"},
		"total_ai_requests": {Used: ptr(77800), Unit: "requests", Window: "all-time"},
		"composer_sessions": {Used: ptr(84), Unit: "sessions", Window: "all-time"},
		"composer_requests": {Used: ptr(645), Unit: "requests", Window: "all-time"},

		"composer_accepted_lines":  {Used: ptr(148), Unit: "lines", Window: "today"},
		"composer_suggested_lines": {Used: ptr(148), Unit: "lines", Window: "today"},
		"tab_accepted_lines":       {Used: ptr(0), Unit: "lines", Window: "today"},
		"tab_suggested_lines":      {Used: ptr(0), Unit: "lines", Window: "today"},

		"billing_cached_tokens": {Used: ptr(63400000), Unit: "tokens", Window: "month"},
		"billing_input_tokens":  {Used: ptr(597100), Unit: "tokens", Window: "month"},
		"billing_output_tokens": {Used: ptr(320100), Unit: "tokens", Window: "month"},

		"ai_deleted_files": {Used: ptr(21), Unit: "files", Window: "all-time"},
		"ai_tracked_files": {Used: ptr(16), Unit: "files", Window: "all-time"},

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

		"interface_composer": {Used: ptr(67400), Unit: "requests", Window: "all-time"},
		"interface_cli":      {Used: ptr(10100), Unit: "requests", Window: "all-time"},
		"interface_human":    {Used: ptr(251), Unit: "requests", Window: "all-time"},
		"interface_tab":      {Used: ptr(97), Unit: "requests", Window: "all-time"},

		"tool_calls_total":  {Used: ptr(30400), Unit: "calls", Window: "all-time"},
		"tool_success_rate": {Used: ptr(95), Unit: "%", Window: "all-time"},
		"tool_completed":    {Used: ptr(28880), Unit: "calls", Window: "all-time"},
		"tool_errored":      {Used: ptr(1216), Unit: "calls", Window: "all-time"},
		"tool_cancelled":    {Used: ptr(304), Unit: "calls", Window: "all-time"},

		"composer_lines_added":   {Used: ptr(74600), Unit: "lines", Window: "all-time"},
		"composer_lines_removed": {Used: ptr(18500), Unit: "lines", Window: "all-time"},
		"composer_files_changed": {Used: ptr(844), Unit: "files", Window: "all-time"},
		"scored_commits":         {Used: ptr(239), Unit: "commits", Window: "all-time"},
		"ai_code_percentage":     {Used: ptr(98), Unit: "%", Window: "all-commits"},
		"total_prompts":          {Used: ptr(898), Unit: "prompts", Window: "all-time"},

		"agentic_sessions":       {Used: ptr(71), Unit: "sessions", Window: "all-time"},
		"non_agentic_sessions":   {Used: ptr(13), Unit: "sessions", Window: "all-time"},
		"composer_files_created": {Used: ptr(312), Unit: "files", Window: "all-time"},
		"composer_files_removed": {Used: ptr(47), Unit: "files", Window: "all-time"},
	}

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
