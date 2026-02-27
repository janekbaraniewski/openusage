package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildClaudeCodeDemoSnapshot(now time.Time) core.UsageSnapshot {
	return core.UsageSnapshot{
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
			"client_webshop_input_tokens":     {Used: ptr(62100000), Unit: "tokens", Window: "7d"},
			"client_webshop_output_tokens":    {Used: ptr(21700000), Unit: "tokens", Window: "7d"},
			"client_webshop_cached_tokens":    {Used: ptr(53200), Unit: "tokens", Window: "7d"},
			"client_webshop_reasoning_tokens": {Used: ptr(13300), Unit: "tokens", Window: "7d"},
			"client_webshop_total_tokens":     {Used: ptr(83800000), Unit: "tokens", Window: "7d"},
			"client_webshop_sessions":         {Used: ptr(42), Unit: "sessions", Window: "7d"},
			"client_webshop_requests":         {Used: ptr(890), Unit: "requests", Window: "7d"},
			"client_docs_site_input_tokens": {
				Used: ptr(50900), Unit: "tokens", Window: "7d",
			},
			"client_docs_site_output_tokens": {
				Used: ptr(17900), Unit: "tokens", Window: "7d",
			},
			"client_docs_site_total_tokens": {Used: ptr(68800), Unit: "tokens", Window: "7d"},
			"client_docs_site_sessions":     {Used: ptr(11), Unit: "sessions", Window: "7d"},
			"client_docs_site_requests":     {Used: ptr(681), Unit: "requests", Window: "7d"},
			"client_backend_api_total_tokens": {
				Used: ptr(67500), Unit: "tokens", Window: "7d",
			},
			"client_backend_api_sessions": {Used: ptr(8), Unit: "sessions", Window: "7d"},
			"client_backend_api_requests": {Used: ptr(773), Unit: "requests", Window: "7d"},
			"client_mobile_app_total_tokens": {
				Used: ptr(51700000), Unit: "tokens", Window: "7d",
			},
			"client_mobile_app_sessions": {Used: ptr(5), Unit: "sessions", Window: "7d"},
			"client_mobile_app_requests": {Used: ptr(492), Unit: "requests", Window: "7d"},
			"client_admin_panel_total_tokens": {
				Used: ptr(35300), Unit: "tokens", Window: "7d",
			},
			"client_admin_panel_sessions": {Used: ptr(3), Unit: "sessions", Window: "7d"},
			"client_admin_panel_requests": {Used: ptr(310), Unit: "requests", Window: "7d"},
			"client_batch_jobs_total_tokens": {
				Used: ptr(28900), Unit: "tokens", Window: "7d",
			},
			"client_batch_jobs_sessions": {Used: ptr(4), Unit: "sessions", Window: "7d"},
			"client_batch_jobs_requests": {Used: ptr(245), Unit: "requests", Window: "7d"},
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
		Attributes: map[string]string{
			"account_email": "demo.user@example.test",
			"plan_type":     "max_5",
			"auth_type":     "api_key",
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
			"client_usage":       "Webshop 24%, Docs Site 20%, Backend API 20%, Mobile App 15%, Admin Panel 10%, Batch Jobs 8%",
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
			"tokens_client_webshop":                demoSeries(now, 21300, 24700, 25900, 28100, 29400, 31800, 34600),
			"tokens_client_docs_site":      demoSeries(now, 6100, 7200, 8000, 8700, 8900, 9800, 11100),
			"tokens_client_backend_api":       demoSeries(now, 10200, 11400, 12600, 13200, 14100, 15200, 16500),
			"tokens_client_mobile_app":              demoSeries(now, 7600, 8100, 8700, 9300, 10100, 10800, 11700),
			"tokens_client_admin_panel":          demoSeries(now, 5200, 6100, 6800, 7200, 7900, 8600, 9400),
			"tokens_client_batch_jobs":               demoSeries(now, 3600, 4000, 4300, 4700, 5100, 5600, 6000),
			"usage_model_claude-opus-4-6":           demoSeries(now, 15, 17, 19, 20, 22, 24, 26),
			"usage_model_claude-haiku-4-5-20251001": demoSeries(now, 2, 3, 3, 4, 4, 5, 5),
		},
		Message: "~$17.69 today · $33.63/h",
	}
}
