package main

import (
	"time"

	"github.com/janekbaraniewski/openusage/internal/core"
)

func buildOllamaDemoSnapshot(now time.Time) core.UsageSnapshot {
	return core.UsageSnapshot{
		ProviderID: "ollama",
		AccountID:  "ollama",
		Timestamp:  now,
		Status:     core.StatusOK,
		Metrics: map[string]core.Metric{
			// Usage gauges (from cloud API / settings page)
			"usage_five_hour": {Used: ptr(34), Limit: ptr(100), Remaining: ptr(66), Unit: "%", Window: "5h"},
			"usage_weekly":    {Used: ptr(61), Limit: ptr(100), Remaining: ptr(39), Unit: "%", Window: "1w"},
			"usage_one_day":   {Used: ptr(61), Limit: ptr(100), Remaining: ptr(39), Unit: "%", Window: "1d"},

			// Model counts
			"models_total":  {Used: ptr(6), Remaining: ptr(6), Unit: "models", Window: "current"},
			"models_local":  {Used: ptr(2), Remaining: ptr(2), Unit: "models", Window: "current"},
			"models_cloud":  {Used: ptr(4), Remaining: ptr(4), Unit: "models", Window: "current"},
			"loaded_models": {Used: ptr(1), Remaining: ptr(1), Unit: "models", Window: "current"},

			// Capabilities (from /api/show)
			"models_with_tools":    {Used: ptr(5), Remaining: ptr(5), Unit: "models", Window: "current"},
			"models_with_vision":   {Used: ptr(2), Remaining: ptr(2), Unit: "models", Window: "current"},
			"models_with_thinking": {Used: ptr(3), Remaining: ptr(3), Unit: "models", Window: "current"},
			"max_context_length":   {Used: ptr(262144), Remaining: ptr(262144), Unit: "tokens", Window: "current"},

			// Request windows
			"requests_5h":    {Used: ptr(47), Remaining: ptr(47), Unit: "requests", Window: "5h"},
			"requests_1d":    {Used: ptr(92), Remaining: ptr(92), Unit: "requests", Window: "1d"},
			"requests_today": {Used: ptr(92), Remaining: ptr(92), Unit: "requests", Window: "today"},
			"requests_7d":    {Used: ptr(318), Remaining: ptr(318), Unit: "requests", Window: "7d"},

			// Token windows (estimated from desktop DB)
			"tokens_5h":    {Used: ptr(12400), Remaining: ptr(12400), Unit: "tokens", Window: "5h"},
			"tokens_1d":    {Used: ptr(28600), Remaining: ptr(28600), Unit: "tokens", Window: "1d"},
			"tokens_today": {Used: ptr(28600), Remaining: ptr(28600), Unit: "tokens", Window: "today"},
			"7d_tokens":    {Used: ptr(94200), Remaining: ptr(94200), Unit: "tokens", Window: "7d"},

			// Activity
			"sessions_5h":      {Used: ptr(3), Remaining: ptr(3), Unit: "sessions", Window: "5h"},
			"sessions_1d":      {Used: ptr(8), Remaining: ptr(8), Unit: "sessions", Window: "1d"},
			"tool_calls_5h":    {Used: ptr(12), Remaining: ptr(12), Unit: "calls", Window: "5h"},
			"tool_calls_1d":    {Used: ptr(31), Remaining: ptr(31), Unit: "calls", Window: "1d"},
			"tool_calls_today": {Used: ptr(31), Remaining: ptr(31), Unit: "calls", Window: "today"},

			// Realtime
			"chat_requests_today":     {Used: ptr(68), Remaining: ptr(68), Unit: "requests", Window: "today"},
			"generate_requests_today": {Used: ptr(24), Remaining: ptr(24), Unit: "requests", Window: "today"},
			"avg_latency_ms_today":    {Used: ptr(2340), Remaining: ptr(2340), Unit: "ms", Window: "today"},
			"thinking_requests":       {Used: ptr(42), Remaining: ptr(42), Unit: "requests", Window: "all-time"},

			// Thinking metrics (from desktop DB)
			"avg_thinking_seconds":   {Used: ptr(8.4), Remaining: ptr(8.4), Unit: "seconds", Window: "all-time"},
			"total_thinking_seconds": {Used: ptr(352.8), Remaining: ptr(352.8), Unit: "seconds", Window: "all-time"},

			// Per-model metrics
			"model_qwen3_coder_480b_cloud_total_tokens":       {Used: ptr(38200), Remaining: ptr(38200), Unit: "tokens", Window: "all-time"},
			"model_qwen3_coder_480b_cloud_requests":           {Used: ptr(45), Remaining: ptr(45), Unit: "requests", Window: "all-time"},
			"model_qwen3_coder_480b_cloud_context_length":     {Used: ptr(262144), Remaining: ptr(262144), Unit: "tokens", Window: "current"},
			"model_deepseek_v3_1_671b_cloud_total_tokens":     {Used: ptr(24100), Remaining: ptr(24100), Unit: "tokens", Window: "all-time"},
			"model_deepseek_v3_1_671b_cloud_requests":         {Used: ptr(28), Remaining: ptr(28), Unit: "requests", Window: "all-time"},
			"model_deepseek_v3_1_671b_cloud_thinking_seconds": {Used: ptr(142.3), Remaining: ptr(142.3), Unit: "seconds", Window: "all-time"},
			"model_qwen3_vl_235b_cloud_total_tokens":          {Used: ptr(18900), Remaining: ptr(18900), Unit: "tokens", Window: "all-time"},
			"model_qwen3_vl_235b_cloud_requests":              {Used: ptr(14), Remaining: ptr(14), Unit: "requests", Window: "all-time"},
			"model_llama3_2_70b_total_tokens":                 {Used: ptr(8400), Remaining: ptr(8400), Unit: "tokens", Window: "all-time"},
			"model_llama3_2_70b_requests":                     {Used: ptr(12), Remaining: ptr(12), Unit: "requests", Window: "all-time"},
			"model_qwen3_32b_total_tokens":                    {Used: ptr(4600), Remaining: ptr(4600), Unit: "tokens", Window: "all-time"},
			"model_qwen3_32b_requests":                        {Used: ptr(9), Remaining: ptr(9), Unit: "requests", Window: "all-time"},

			// Client composition
			"client_cloud_total_tokens": {Used: ptr(81200), Remaining: ptr(81200), Unit: "tokens", Window: "all-time"},
			"client_cloud_requests":     {Used: ptr(87), Remaining: ptr(87), Unit: "requests", Window: "all-time"},
			"client_cloud_sessions":     {Used: ptr(15), Remaining: ptr(15), Unit: "sessions", Window: "all-time"},
			"client_local_total_tokens": {Used: ptr(13000), Remaining: ptr(13000), Unit: "tokens", Window: "all-time"},
			"client_local_requests":     {Used: ptr(21), Remaining: ptr(21), Unit: "requests", Window: "all-time"},
			"client_local_sessions":     {Used: ptr(6), Remaining: ptr(6), Unit: "sessions", Window: "all-time"},

			// Source composition
			"source_cloud_requests":       {Used: ptr(87), Remaining: ptr(87), Unit: "requests", Window: "all-time"},
			"source_cloud_requests_today": {Used: ptr(68), Remaining: ptr(68), Unit: "requests", Window: "today"},
			"source_local_requests":       {Used: ptr(21), Remaining: ptr(21), Unit: "requests", Window: "all-time"},

			// Provider composition
			"provider_cloud_total_tokens": {Used: ptr(81200), Remaining: ptr(81200), Unit: "tokens", Window: "all-time"},
			"provider_local_total_tokens": {Used: ptr(13000), Remaining: ptr(13000), Unit: "tokens", Window: "all-time"},

			// Tool usage
			"tool_web_search":    {Used: ptr(8), Remaining: ptr(8), Unit: "calls", Window: "all-time"},
			"tool_code_analysis": {Used: ptr(15), Remaining: ptr(15), Unit: "calls", Window: "all-time"},
			"tool_file_edit":     {Used: ptr(6), Remaining: ptr(6), Unit: "calls", Window: "all-time"},

			// Misc
			"messages_today": {Used: ptr(92), Remaining: ptr(92), Unit: "messages", Window: "today"},
			"messages_5h":    {Used: ptr(47), Remaining: ptr(47), Unit: "messages", Window: "5h"},
			"messages_1d":    {Used: ptr(92), Remaining: ptr(92), Unit: "messages", Window: "1d"},
		},
		Resets: map[string]time.Time{
			"usage_five_hour": now.Add(3*time.Hour + 12*time.Minute),
			"usage_weekly":    now.Add(4*24*time.Hour + 6*time.Hour),
			"usage_one_day":   now.Add(4*24*time.Hour + 6*time.Hour),
		},
		Attributes: map[string]string{
			"account_email":    "demo@ollama.ai",
			"account_name":     "demo_user",
			"plan_name":        "pro",
			"cli_version":      "0.16.3",
			"selected_model":   "qwen3-coder:480b-cloud",
			"cloud_disabled":   "false",
			"cloud_source":     "none",
			"token_estimation": "chars_div_4",
			"block_start":      now.Add(-2 * time.Hour).Format(time.RFC3339),
			"block_end":        now.Add(3*time.Hour + 12*time.Minute).Format(time.RFC3339),
			"model_qwen3_coder_480b_cloud_capability_tools":      "true",
			"model_deepseek_v3_1_671b_cloud_capability_tools":    "true",
			"model_deepseek_v3_1_671b_cloud_capability_thinking": "true",
			"model_qwen3_vl_235b_cloud_capability_vision":        "true",
			"model_qwen3_vl_235b_cloud_capability_tools":         "true",
			"model_qwen3_vl_235b_cloud_capability_thinking":      "true",
		},
		Raw: map[string]string{
			"account_email":              "demo@ollama.ai",
			"account_name":               "demo_user",
			"plan_name":                  "pro",
			"selected_model":             "qwen3-coder:480b-cloud",
			"cloud_disabled":             "false",
			"cloud_source":               "none",
			"cli_version":                "0.16.3",
			"models_usage_top":           "qwen3-coder:480b-cloud=45, deepseek-v3.1:671b-cloud=28, qwen3-vl:235b-cloud=14, llama3.2:70b=12, qwen3:32b=9",
			"model_tokens_estimated_top": "qwen3-coder:480b-cloud=38200, deepseek-v3.1:671b-cloud=24100, qwen3-vl:235b-cloud=18900, llama3.2:70b=8400",
			"tool_usage":                 "web_search=8, code_analysis=15, file_edit=6",
			"token_estimation":           "chars_div_4",
		},
		ModelUsage: []core.ModelUsageRecord{
			{RawModelID: "qwen3-coder:480b-cloud", Window: "all-time", Requests: ptr(45), InputTokens: ptr(4200), OutputTokens: ptr(34000), Dimensions: map[string]string{"provider": "ollama", "estimation": "chars_div_4"}},
			{RawModelID: "deepseek-v3.1:671b-cloud", Window: "all-time", Requests: ptr(28), InputTokens: ptr(2800), OutputTokens: ptr(21300), Dimensions: map[string]string{"provider": "ollama", "estimation": "chars_div_4"}},
			{RawModelID: "qwen3-vl:235b-cloud", Window: "all-time", Requests: ptr(14), InputTokens: ptr(1900), OutputTokens: ptr(17000), Dimensions: map[string]string{"provider": "ollama", "estimation": "chars_div_4"}},
			{RawModelID: "llama3.2:70b", Window: "all-time", Requests: ptr(12), InputTokens: ptr(1200), OutputTokens: ptr(7200), Dimensions: map[string]string{"provider": "ollama", "estimation": "chars_div_4"}},
			{RawModelID: "qwen3:32b", Window: "all-time", Requests: ptr(9), InputTokens: ptr(600), OutputTokens: ptr(4000), Dimensions: map[string]string{"provider": "ollama", "estimation": "chars_div_4"}},
		},
		DailySeries: map[string][]core.TimePoint{
			"analytics_tokens":                      demoSeries(now, 8200, 12400, 18600, 24100, 32400, 41200, 54800),
			"analytics_requests":                    demoSeries(now, 12, 18, 28, 36, 48, 62, 78),
			"tokens_model_qwen3_coder_480b_cloud":   demoSeries(now, 3200, 5100, 8200, 11400, 16200, 22400, 28600),
			"tokens_model_deepseek_v3_1_671b_cloud": demoSeries(now, 2400, 3800, 5600, 7200, 9800, 12400, 16200),
			"tokens_model_qwen3_vl_235b_cloud":      demoSeries(now, 1800, 2600, 3400, 4200, 5200, 6400, 8000),
			"usage_client_cloud":                    demoSeries(now, 6200, 10400, 16200, 22000, 30100, 38400, 50600),
			"usage_client_local":                    demoSeries(now, 2000, 2000, 2400, 2100, 2300, 2800, 4200),
			"usage_source_cloud":                    demoSeries(now, 8, 14, 22, 30, 42, 54, 68),
			"usage_source_local":                    demoSeries(now, 4, 4, 6, 6, 6, 8, 10),
		},
		Message: "Ollama · 6 models · 34% 5h · 61% weekly",
	}
}
