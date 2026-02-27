package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildGeminiCLIDemoSnapshot(now time.Time) core.UsageSnapshot {
	return core.UsageSnapshot{
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
			"tool_calls_total":   {Used: ptr(206), Unit: "calls", Window: "7d"},
			"tool_completed":     {Used: ptr(185), Unit: "calls", Window: "7d"},
			"tool_errored":       {Used: ptr(16), Unit: "calls", Window: "7d"},
			"tool_cancelled":     {Used: ptr(5), Unit: "calls", Window: "7d"},
			"tool_success_rate":  {Used: ptr(89.8), Unit: "%", Window: "7d"},
			"total_prompts":      {Used: ptr(1730), Unit: "prompts", Window: "all-time"},
			"composer_lines_added": {
				Used: ptr(824), Unit: "lines", Window: "7d",
			},
			"composer_lines_removed": {
				Used: ptr(291), Unit: "lines", Window: "7d",
			},
			"composer_files_changed": {Used: ptr(58), Unit: "files", Window: "7d"},
			"scored_commits":         {Used: ptr(9), Unit: "commits", Window: "7d"},
			"ai_code_percentage": {
				Used: ptr(100), Limit: ptr(100), Remaining: ptr(0), Unit: "%", Window: "7d",
			},
			"tool_google_web_search": {
				Used: ptr(48), Unit: "calls", Window: "7d",
			},
			"tool_run_shell_command": {
				Used: ptr(41), Unit: "calls", Window: "7d",
			},
			"tool_read_file": {Used: ptr(34), Unit: "calls", Window: "7d"},
			"tool_replace":   {Used: ptr(28), Unit: "calls", Window: "7d"},
			"tool_write_file": {
				Used: ptr(17), Unit: "calls", Window: "7d",
			},
			"lang_go":         {Used: ptr(73), Unit: "requests", Window: "7d"},
			"lang_markdown":   {Used: ptr(31), Unit: "requests", Window: "7d"},
			"lang_yaml":       {Used: ptr(19), Unit: "requests", Window: "7d"},
			"lang_typescript": {Used: ptr(12), Unit: "requests", Window: "7d"},
		},
		Resets: map[string]time.Time{
			"quota_model_gemini_2_5_pro_requests_reset":        now.Add(22*time.Hour + 9*time.Minute),
			"quota_model_gemini_2_0_flash_requests_reset":      now.Add(7*time.Hour + 3*time.Minute),
			"quota_model_gemini_2_5_flash_lite_requests_reset": now.Add(7*time.Hour + 3*time.Minute),
			"quota_reset": now.Add(22*time.Hour + 9*time.Minute),
		},
		Attributes: map[string]string{
			"auth_type":   "oauth",
			"cli_version": "0.4.21",
			"account_id":  "demo-gemini-user",
		},
		Raw: map[string]string{
			"oauth_status":   "valid (refreshed)",
			"quota_api":      "ok (22 buckets)",
			"auth_type":      "oauth",
			"model_usage":    "gemini-3-pro-preview: 75%, gemini-3-flash-preview: 25%",
			"client_usage":   "CLI 100%",
			"tool_usage":     "google_web_search (48), run_shell_command (41), read_file (34), replace (28), write_file (17)",
			"language_usage": "go: 73 req, markdown: 31 req, yaml: 19 req, typescript: 12 req",
		},
		ModelUsage: []core.ModelUsageRecord{
			{
				RawModelID:       "gemini-2.5-pro",
				Canonical:        "gemini-2.5-pro",
				CanonicalFamily:  "gemini",
				CanonicalVariant: "pro",
				CanonicalVendor:  "google",
				InputTokens:      ptr(146700),
				OutputTokens:     ptr(23800),
				CachedTokens:     ptr(33600),
				ReasoningTokens:  ptr(20600),
				Window:           "7d",
				Confidence:       1.0,
			},
			{
				RawModelID:       "gemini-2.0-flash",
				Canonical:        "gemini-2.0-flash",
				CanonicalFamily:  "gemini",
				CanonicalVariant: "flash",
				CanonicalVendor:  "google",
				InputTokens:      ptr(36100),
				OutputTokens:     ptr(9300),
				Window:           "7d",
				Confidence:       1.0,
			},
		},
		DailySeries: map[string][]core.TimePoint{
			"tokens_client_cli": demoSeries(now, 17100, 18300, 19800, 21200, 22600, 24300, 25100),
			"tool_calls":        demoSeries(now, 17, 19, 24, 22, 28, 32, 36),
			"tokens_total":      demoSeries(now, 17100, 18300, 19800, 21200, 22600, 24300, 25100),
			"requests":          demoSeries(now, 29, 31, 36, 34, 40, 42, 45),
			"analytics_tokens":  demoSeries(now, 4.8e6, 5.0e6, 5.1e6, 5.3e6, 5.4e6, 5.5e6, 5.7e6),
		},
		Message: "",
	}
}
